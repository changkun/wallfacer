package executor

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"

	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/logger"
)

// launchOpenCode runs the opencode CLI in host mode. opencode's
// `run --format json` emits NDJSON but never a terminal result event: the run
// loop simply breaks when the session goes idle. The runner downstream needs a
// terminal KindResult to derive the agent output (and trigger a commit), so we
// tee opencode's stdout, accumulate the final text + token usage from its
// `text` / `step_finish` events, and append a synthesized {"type":"result"}
// line that harness.OpenCode.ParseEvent maps to KindResult. This mirrors the
// codex output-last-message path.
//
// Two opencode-specific adjustments to the shared Request:
//
//   - Permission is forced to Full so opencode runs with
//     --dangerously-skip-permissions; without it opencode blocks on an
//     interactive approval prompt and the task never produces a commit.
//   - SystemPrompt carries the instructions file *contents*, not its path.
//     requestFromClaudeSpec sets the path; opencode has no append-system-prompt
//     flag (Capabilities.SupportsSystemPrompt == false) so the harness prepends
//     SystemPrompt into the prompt. This mirrors launchCodex / launchCursor.
func (b *HostBackend) launchOpenCode(ctx context.Context, spec ContainerSpec) (Handle, error) {
	bin, err := b.binaryFor(harness.OpenCode)
	if err != nil {
		return nil, err
	}

	env := b.buildChildEnv(spec)
	req := requestFromClaudeSpec(spec)
	if req.Prompt == "" {
		return nil, fmt.Errorf("host backend: opencode launch requires a -p <prompt> argument in spec.Cmd")
	}
	req.Permission = harness.PermissionFull
	req.Cwd = spec.WorkDir

	if instrPath := spec.Env["WALLFACER_INSTRUCTIONS_PATH"]; instrPath != "" {
		data, rErr := os.ReadFile(instrPath)
		if rErr != nil {
			logger.Runner.Warn("host backend: read instructions file", "path", instrPath, "error", rErr)
		}
		req.SystemPrompt = string(data)
	}

	openCodeH, _ := harness.Lookup(harness.OpenCode)
	argv, _, argvErr := openCodeH.BuildArgv(req)
	if argvErr != nil {
		return nil, fmt.Errorf("host backend: opencode argv: %w", argvErr)
	}

	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Env = env
	if spec.WorkDir != "" {
		cmd.Dir = spec.WorkDir
	}

	// opencode's real stdout is consumed internally; a pipe exposes the tee'd
	// stream (events + synthesized result) to the runner.
	ocStdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	ocStderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	pipeR, pipeW := io.Pipe()

	taskID := spec.Labels["wallfacer.task.id"]
	h := newHostHandle(spec.Name, cmd, pipeR, ocStderr, taskID, b)

	if err := cmd.Start(); err != nil {
		transition(&h.state, StateFailed)
		return nil, fmt.Errorf("start host agent: %w", err)
	}
	transition(&h.state, StateRunning)

	b.procMu.Lock()
	b.procs[spec.Name] = h
	b.procMu.Unlock()

	go teeOpenCodeAndAppendResult(ocStdout, pipeW)

	return h, nil
}

// openCodeResultRecord is the synthesized terminal line appended after
// opencode's stdout closes. Its shape matches what harness.OpenCode.ParseEvent
// recognises as a KindResult (type "result", top-level result/usage/cost).
type openCodeResultRecord struct {
	Type       string             `json:"type"`
	SessionID  string             `json:"sessionID"`
	Result     string             `json:"result"`
	IsError    bool               `json:"is_error"`
	StopReason string             `json:"stop_reason"`
	Usage      openCodeUsageBlock `json:"usage"`
	Cost       float64            `json:"cost"`
}

// openCodeUsageBlock is opencode's token-accounting shape.
type openCodeUsageBlock struct {
	Input     int `json:"input"`
	Output    int `json:"output"`
	Reasoning int `json:"reasoning"`
	Cache     struct {
		Read  int `json:"read"`
		Write int `json:"write"`
	} `json:"cache"`
}

// openCodeSniff captures the fields the tee needs from each opencode event.
// Unknown fields are ignored, so this is forward-compatible.
type openCodeSniff struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionID"`
	Part      *struct {
		Text   string              `json:"text"`
		Cost   float64             `json:"cost"`
		Tokens *openCodeUsageBlock `json:"tokens"`
	} `json:"part"`
}

// teeOpenCodeAndAppendResult forwards each opencode stdout line to `out` while
// accumulating the final text + token usage. When opencode's stdout closes
// (EOF), it synthesizes a {"type":"result"} record and writes it as the final
// line so the runner's harness parser sees a terminal KindResult.
//
// opencode buffers its JSON output and flushes at the end of the run; the
// scanner reads whatever arrives and the synthesis always runs on EOF, so the
// buffering does not change correctness.
func teeOpenCodeAndAppendResult(ocStdout io.Reader, out *io.PipeWriter) {
	record := openCodeResultRecord{Type: "result", StopReason: "end_turn"}
	hadStdout := false
	sawError := false

	scanner := bufio.NewScanner(ocStdout)
	// opencode events can be large (full assistant messages / tool output);
	// lift the default 64 KiB line cap to 1 MiB.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		hadStdout = true
		line := scanner.Bytes()

		if _, err := out.Write(append([]byte{}, line...)); err != nil {
			_ = out.CloseWithError(err)
			return
		}
		if _, err := out.Write([]byte("\n")); err != nil {
			_ = out.CloseWithError(err)
			return
		}

		var evt openCodeSniff
		if err := json.Unmarshal(line, &evt); err != nil {
			continue
		}
		if evt.SessionID != "" {
			record.SessionID = evt.SessionID
		}
		switch evt.Type {
		case "text":
			if evt.Part != nil {
				record.Result = evt.Part.Text
			}
		case "step_finish":
			if evt.Part != nil {
				record.Cost += evt.Part.Cost
				if t := evt.Part.Tokens; t != nil {
					record.Usage.Input += t.Input
					record.Usage.Output += t.Output
					record.Usage.Reasoning += t.Reasoning
					record.Usage.Cache.Read += t.Cache.Read
					record.Usage.Cache.Write += t.Cache.Write
				}
			}
		case "error":
			sawError = true
		}
	}
	if err := scanner.Err(); err != nil {
		logger.Runner.Warn("host backend: scan opencode stdout", "error", err)
	}

	// Treat a run that produced no final text as a failure when the stream was
	// empty or carried a session error, so the runner does not record an empty
	// result as success.
	if record.Result == "" && (sawError || !hadStdout) {
		record.IsError = true
		record.StopReason = "error_during_execution"
	}

	final, err := json.Marshal(record)
	if err != nil {
		_ = out.CloseWithError(fmt.Errorf("marshal opencode result: %w", err))
		return
	}
	if _, err := out.Write(final); err != nil {
		_ = out.CloseWithError(err)
		return
	}
	_, _ = out.Write([]byte("\n"))
	_ = out.Close()
}
