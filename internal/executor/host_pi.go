package executor

import (
	"context"

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
	return b.launchPlainHostAgent(ctx, spec, plainHostLaunch{
		id:            harness.Pi,
		requirePrompt: true,
		forceFull:     true,
	})
}
