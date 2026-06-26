package adversarial

import (
	"latere.ai/x/agon/pkg/adversarial"
	agonClaude "latere.ai/x/agon/pkg/adversarial/claude"
)

// NewSessionProposer returns a Proposer backed by the claude fork-session path.
// sessionID is Task.SessionID; cwd is the task's working directory.
// Returns nil if sessionID is empty — callers must check.
//
// The proposer runs in the task's real worktree (fork-session is cwd-scoped, so
// it cannot run elsewhere). It is effectively read-only today because this path
// does not pass --dangerously-skip-permissions, so claude default-denies write
// tools in headless mode. agon spec 38 adds an explicit guarantee via
// agonClaude.WithProposerReadOnly(); wire it here once wallfacer's go.mod is
// bumped to the agon release that contains it.
func NewSessionProposer(sessionID, cwd string) adversarial.Proposer {
	if sessionID == "" {
		return nil
	}
	return agonClaude.NewProposer(sessionID, cwd)
}
