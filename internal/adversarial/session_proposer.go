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
// it cannot run elsewhere) and is restricted to read-only tools
// (agonClaude.WithProposerReadOnly, agon spec 38): it can read the code to
// rebut and concede but cannot edit the tree wallfacer's commit pipeline would
// then stage. This is an explicit guarantee on top of claude's headless
// default-deny.
func NewSessionProposer(sessionID, cwd string) adversarial.Proposer {
	if sessionID == "" {
		return nil
	}
	return agonClaude.NewProposer(sessionID, cwd, agonClaude.WithProposerReadOnly())
}
