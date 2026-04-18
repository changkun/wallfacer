package sandbox

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"changkun.de/x/wallfacer/internal/logger"
)

// launchCodex runs the codex CLI in host mode. The runner emits
// Claude-style argv (`-p <prompt> --verbose --output-format stream-json
// [--model <m>] [--resume <sid>]`); codex's own argv is different, so we
// translate it and wrap the output.
//
// Translation, mirroring the container image's codex-agent.sh:
//
//	codex exec --full-auto --sandbox workspace-write --skip-git-repo-check
//	     --json --output-last-message <tmp> --color never
//	     [--config model_reasoning_effort="low"]   (when WALLFACER_SANDBOX_FAST != "false")
//	     [--model <m>]
//	     <prompt>
//
// The runner downstream parses the final NDJSON line from stdout as a
// Claude result record. Codex doesn't emit that record natively, so we
// scan its JSON event stream for session_id / stop_reason / usage / cost,
// read the last-message file after exit, and append a synthesized
// Claude-compatible record as the final stdout line.
//
// Session resume: codex's exec subcommand does not currently support
// resuming a prior session via a stable flag; we ignore --resume here and
// accept that codex host-mode is stateless per turn. The container path
// also drops --resume (see codex-agent.sh), so this matches existing
// container behaviour.
func (b *HostBackend) launchCodex(ctx context.Context, spec ContainerSpec) (Handle, error) {
	bin, err := b.binaryFor(Codex)
	if err != nil {
		return nil, err
	}

	env := b.buildChildEnv(spec)

	prompt, model := extractPromptAndModelFromClaudeArgv(spec.Cmd)
	if prompt == "" {
		return nil, fmt.Errorf("host backend: codex launch requires a -p <prompt> argument in spec.Cmd")
	}
	if model == "" {
		// Fall back to CODEX_DEFAULT_MODEL from env if set (matches
		// codex-agent.sh behaviour).
		for _, kv := range env {
			if strings.HasPrefix(kv, "CODEX_DEFAULT_MODEL=") {
				model = strings.TrimPrefix(kv, "CODEX_DEFAULT_MODEL=")
				break
			}
		}
	}

	// Apply the optional instructions preamble the same way the claude
	// path does: prepend to the prompt. Codex's exec CLI does not accept
	// --append-system-prompt today, so feature-probing is pointless — we
	// just prepend when an instructions file is present.
	if instrPath := spec.Env["WALLFACER_INSTRUCTIONS_PATH"]; instrPath != "" {
		if data, rErr := os.ReadFile(instrPath); rErr == nil && len(data) > 0 {
			prompt = string(data) + "\n\n---\n\n" + prompt
		} else if rErr != nil {
			logger.Runner.Warn("host backend: read instructions file", "path", instrPath, "error", rErr)
		}
	}

	// Per-launch temp dir holds the --output-last-message file. Using a
	// fresh dir per call avoids races that the container script had with
	// its fixed /tmp/codex-* paths.
	tmpDir, err := os.MkdirTemp("", "wallfacer-codex-")
	if err != nil {
		return nil, fmt.Errorf("host backend: codex tmp dir: %w", err)
	}
	lastMsgFile := filepath.Join(tmpDir, "last-message.txt")

	// Assemble codex argv.
	argv := []string{
		"exec",
		"--full-auto",
		"--sandbox", "workspace-write",
		"--skip-git-repo-check",
		"--json",
		"--output-last-message", lastMsgFile,
		"--color", "never",
	}
	if sandboxFast(spec.Env, env) {
		argv = append(argv, "--config", `model_reasoning_effort="low"`)
	}
	if model != "" {
		argv = append(argv, "--model", model)
	}
	argv = append(argv, prompt)

	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Env = env
	if spec.WorkDir != "" {
		cmd.Dir = spec.WorkDir
	}

	// codex's real stdout is consumed internally; a pipe exposes the tee'd
	// stream (events + synthesized result) to the runner.
	codexStdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	codexStderr, err := cmd.StderrPipe()
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	pipeR, pipeW := io.Pipe()

	taskID := spec.Labels["wallfacer.task.id"]
	h := newHostHandle(spec.Name, cmd, pipeR, codexStderr, taskID, b)

	if err := cmd.Start(); err != nil {
		_ = os.RemoveAll(tmpDir)
		transition(&h.state, StateFailed)
		return nil, fmt.Errorf("start host agent: %w", err)
	}
	transition(&h.state, StateRunning)

	b.procMu.Lock()
	b.procs[spec.Name] = h
	b.procMu.Unlock()

	// Tee + wrap goroutine: forwards each codex stdout line to the runner
	// while tracking the event fields we need to build the final result.
	// When codex's stdout closes (process exit), appends the synthesized
	// Claude-compatible record and closes the pipe.
	go teeCodexAndAppendResult(codexStdout, pipeW, lastMsgFile, tmpDir)

	return h, nil
}

// extractPromptAndModelFromClaudeArgv finds the `-p <value>` and optional
// `--model <value>` in the runner's Cmd slice. Returns ("", "") when -p
// is missing. Other flags (--verbose, --output-format, --resume) are
// ignored — codex has no equivalents in exec mode.
func extractPromptAndModelFromClaudeArgv(cmd []string) (prompt, model string) {
	for i := 0; i < len(cmd); i++ {
		switch cmd[i] {
		case "-p":
			if i+1 < len(cmd) {
				prompt = cmd[i+1]
				i++
			}
		case "--model", "-m":
			if i+1 < len(cmd) {
				model = cmd[i+1]
				i++
			}
		}
	}
	return prompt, model
}

// sandboxFast reports whether WALLFACER_SANDBOX_FAST is effectively true.
// Defaults to true when unset — matching the container-side default.
func sandboxFast(specEnv map[string]string, childEnv []string) bool {
	if v, ok := specEnv["WALLFACER_SANDBOX_FAST"]; ok {
		return v != "false"
	}
	for _, kv := range childEnv {
		if strings.HasPrefix(kv, "WALLFACER_SANDBOX_FAST=") {
			return strings.TrimPrefix(kv, "WALLFACER_SANDBOX_FAST=") != "false"
		}
	}
	return true
}

// codexResultRecord is the Claude-compatible envelope the runner expects as
// the last NDJSON line. Fields mirror agentOutput in internal/runner.
type codexResultRecord struct {
	Result       string     `json:"result"`
	SessionID    string     `json:"session_id"`
	StopReason   string     `json:"stop_reason"`
	IsError      bool       `json:"is_error"`
	TotalCostUSD float64    `json:"total_cost_usd"`
	Usage        codexUsage `json:"usage"`
	Subtype      string     `json:"subtype,omitempty"`
}

type codexUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// codexEvent captures the union of fields we sniff from codex's JSON events.
// Unknown fields are ignored by json.Unmarshal, so this is forward-compatible.
type codexEvent struct {
	Type         string   `json:"type"`
	SessionID    string   `json:"session_id,omitempty"`
	StopReason   string   `json:"stop_reason,omitempty"`
	TotalCostUSD *float64 `json:"total_cost_usd,omitempty"`
	Usage        *struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CachedInputTokens        int `json:"cached_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage,omitempty"`
}

// teeCodexAndAppendResult forwards each codex stdout line to `out` while
// tracking event metadata. When codex's stdout closes (EOF), synthesizes a
// Claude-compatible final record from the tracked fields and the
// last-message file, writes it, and closes `out` + cleans up tmpDir.
//
// Runs in its own goroutine so the runner's io.ReadAll on the handle's
// stdout gets codex's native events live plus the final envelope at the end.
func teeCodexAndAppendResult(codexStdout io.Reader, out *io.PipeWriter, lastMsgFile, tmpDir string) {
	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	record := codexResultRecord{StopReason: "end_turn"}
	hadStdout := false

	scanner := bufio.NewScanner(codexStdout)
	// Codex events can be large (full assistant messages); lift the default
	// 64 KiB line cap. 1 MiB is well above any realistic event.
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		hadStdout = true
		line := scanner.Bytes()

		// Forward the raw line to the caller.
		if _, err := out.Write(append([]byte{}, line...)); err != nil {
			// Caller closed the read side; drop remaining output and bail.
			_ = out.CloseWithError(err)
			return
		}
		if _, err := out.Write([]byte("\n")); err != nil {
			_ = out.CloseWithError(err)
			return
		}

		// Peek at the event for fields we need.
		var evt codexEvent
		if err := json.Unmarshal(line, &evt); err != nil {
			continue
		}
		if evt.SessionID != "" {
			record.SessionID = evt.SessionID
		}
		if evt.StopReason != "" {
			record.StopReason = evt.StopReason
		}
		if evt.TotalCostUSD != nil {
			record.TotalCostUSD = *evt.TotalCostUSD
		}
		if evt.Type == "turn.completed" && evt.Usage != nil {
			u := evt.Usage
			record.Usage.InputTokens = u.InputTokens
			record.Usage.OutputTokens = u.OutputTokens
			// Codex uses cached_input_tokens; Claude expects
			// cache_read_input_tokens. Accept either.
			if u.CacheReadInputTokens > 0 {
				record.Usage.CacheReadInputTokens = u.CacheReadInputTokens
			} else {
				record.Usage.CacheReadInputTokens = u.CachedInputTokens
			}
			record.Usage.CacheCreationInputTokens = u.CacheCreationInputTokens
		}
	}
	if err := scanner.Err(); err != nil {
		logger.Runner.Warn("host backend: scan codex stdout", "error", err)
	}

	// Build the synthesized final record.
	lastMsg, _ := os.ReadFile(lastMsgFile)
	record.Result = strings.TrimSpace(string(lastMsg))

	// If codex produced nothing useful, mark it as an error so the runner
	// doesn't silently treat an empty result as success.
	if !hadStdout && record.Result == "" {
		record.IsError = true
		record.Subtype = "error_during_execution"
	}

	final, err := json.Marshal(record)
	if err != nil {
		_ = out.CloseWithError(fmt.Errorf("marshal codex result: %w", err))
		return
	}
	if _, err := out.Write(final); err != nil {
		_ = out.CloseWithError(err)
		return
	}
	_, _ = out.Write([]byte("\n"))
	_ = out.Close()
}
