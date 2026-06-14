package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/logger"
)

// launchCursor execs the cursor-agent CLI. Cursor emits Claude-style
// stream-json natively (its terminal `result` event carries the session id,
// final text, and usage), so the plumbing matches launchClaude: a plain
// stdout pipe with no output-last-message wrapping.
//
// Two cursor-specific adjustments to the shared Request:
//
//   - Permission is forced to Full. The host backend always runs with write
//     access; claude and codex bake that into their argv, but cursor reads
//     req.Permission to decide whether to inject --force. Without --force
//     cursor only *proposes* edits and exits without writing, so a task
//     would never produce a commit.
//   - SystemPrompt carries the instructions file *contents*, not its path.
//     requestFromClaudeSpec sets the path; cursor has no append-system-prompt
//     flag (Capabilities.SupportsSystemPrompt == false) so the harness
//     prepends SystemPrompt into the -p value. Passing the path would glue
//     the literal path onto the prompt. This mirrors launchCodex.
func (b *HostBackend) launchCursor(ctx context.Context, spec ContainerSpec) (Handle, error) {
	bin, err := b.binaryFor(harness.Cursor)
	if err != nil {
		return nil, err
	}

	env := b.buildChildEnv(spec)
	req := requestFromClaudeSpec(spec, env)
	if req.Prompt == "" {
		return nil, fmt.Errorf("host backend: cursor launch requires a -p <prompt> argument in spec.Cmd")
	}
	req.Permission = harness.PermissionFull

	if instrPath := spec.Env["WALLFACER_INSTRUCTIONS_PATH"]; instrPath != "" {
		data, rErr := os.ReadFile(instrPath)
		if rErr != nil {
			logger.Runner.Warn("host backend: read instructions file", "path", instrPath, "error", rErr)
		}
		req.SystemPrompt = string(data)
	}

	cursorH, _ := harness.Lookup(harness.Cursor)
	argv, _, argvErr := cursorH.BuildArgv(req)
	if argvErr != nil {
		return nil, fmt.Errorf("host backend: cursor argv: %w", argvErr)
	}

	cmd := exec.CommandContext(ctx, bin, argv...)
	cmd.Env = env
	if spec.WorkDir != "" {
		cmd.Dir = spec.WorkDir
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}

	taskID := spec.Labels["wallfacer.task.id"]
	h := newHostHandle(spec.Name, cmd, stdout, stderr, taskID, b)

	if err := cmd.Start(); err != nil {
		transition(&h.state, StateFailed)
		return nil, fmt.Errorf("start host agent: %w", err)
	}
	transition(&h.state, StateRunning)

	b.procMu.Lock()
	b.procs[spec.Name] = h
	b.procMu.Unlock()

	return h, nil
}
