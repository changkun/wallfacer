package adversarial

import (
	"latere.ai/x/agon/pkg/adversarial"
	agonClaude "latere.ai/x/agon/pkg/adversarial/claude"
)

// NewSessionProposer returns a Proposer backed by the claude fork-session path.
// sessionID is Task.SessionID; cwd is the task's working directory.
// Returns nil if sessionID is empty — callers must check.
func NewSessionProposer(sessionID, cwd string) adversarial.Proposer {
	if sessionID == "" {
		return nil
	}
	return agonClaude.NewProposer(sessionID, cwd)
}
