package executor

import (
	"context"

	"latere.ai/x/wallfacer/internal/harness"
)

// launchCursor execs the cursor-agent CLI. Cursor emits Claude-style
// stream-json natively (its terminal `result` event carries the session id,
// final text, and usage), so the plumbing matches launchClaude: a plain
// stdout pipe with no output-last-message wrapping.
//
// One cursor-specific adjustment to the shared Request:
//
//   - Permission is forced to Full. The host backend always runs with write
//     access; claude and codex bake that into their argv, but cursor reads
//     req.Permission to decide whether to inject --force. Without --force
//     cursor only *proposes* edits and exits without writing, so a task
//     would never produce a commit.
func (b *HostBackend) launchCursor(ctx context.Context, spec ContainerSpec) (Handle, error) {
	return b.launchPlainHostAgent(ctx, spec, plainHostLaunch{
		id:            harness.Cursor,
		requirePrompt: true,
		forceFull:     true,
	})
}
