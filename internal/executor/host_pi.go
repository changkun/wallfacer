package executor

import (
	"context"
	"fmt"
	"os/exec"

	"latere.ai/x/wallfacer/internal/harness"
)

// launchPi execs the pi CLI. Pi emits a canonical JSON event stream natively
// under --mode json (its terminal agent_end carries the final message,
// stop reason, and usage), so the plumbing matches launchClaude/launchCursor:
// a plain stdout pipe with no output-last-message wrapping.
//
// One pi-specific adjustment to the shared Request:
//
//   - Permission is forced to Full. The host backend always runs with write
//     access; pi reads req.Permission to decide its --tools allowlist, and
//     anything below Full would withhold Write/Edit/Bash so a task could
//     never produce a commit. Full omits --tools, enabling all four tools.
func (b *HostBackend) launchPi(ctx context.Context, spec ContainerSpec) (Handle, error) {
	bin, err := b.binaryFor(harness.Pi)
	if err != nil {
		return nil, err
	}

	env := b.buildChildEnv(spec)
	req := requestFromClaudeSpec(spec)
	if req.Prompt == "" {
		return nil, fmt.Errorf("host backend: pi launch requires a -p <prompt> argument in spec.Cmd")
	}
	req.Permission = harness.PermissionFull

	piH, _ := harness.Lookup(harness.Pi)
	argv, _, argvErr := piH.BuildArgv(req)
	if argvErr != nil {
		return nil, fmt.Errorf("host backend: pi argv: %w", argvErr)
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
