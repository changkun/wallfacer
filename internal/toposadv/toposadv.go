// Package toposadv is the single wallfacer seam onto the topos adversarial
// engine (latere.ai/x/topos/adversarial and its claude proposer). It re-exports
// the engine's integration surface as type aliases so the rest of wallfacer
// depends on this package rather than naming the topos engine directly. The
// embeddable-boundary guard (internal/agentgraph) permits the topos adversarial
// imports only here; every other wallfacer package stays behind this seam.
package toposadv

import (
	"latere.ai/x/topos/adversarial"
	reviewClaude "latere.ai/x/topos/adversarial/claude"
)

// Integration types re-exported from the topos adversarial engine as type
// aliases (identical types), so values, interface satisfaction, and struct
// fields carry through unchanged.

// Verifier is the review-owned verification interface a task's runner implements.
type Verifier = adversarial.Verifier

// VerifyInput is the input to a Verifier.Verify call.
type VerifyInput = adversarial.VerifyInput

// VerifyResult is the outcome of a Verifier.Verify call.
type VerifyResult = adversarial.VerifyResult

// Critic is one adversarial critic in the debate protocol.
type Critic = adversarial.Critic

// CriticInput is the input to a Critic.Round call.
type CriticInput = adversarial.CriticInput

// CriticResult is the outcome of a Critic.Round call.
type CriticResult = adversarial.CriticResult

// Proposer is the debate proposer (the implementation under review).
type Proposer = adversarial.Proposer

// Engine drives the proposer/critic adversarial debate to a Summary.
type Engine = adversarial.Engine

// TokenUsage is the per-call token accounting a critic reports back.
type TokenUsage = adversarial.TokenUsage

// AssemblePrompt builds the critic prompt from a CriticInput.
func AssemblePrompt(in CriticInput) string { return adversarial.AssemblePrompt(in) }

// NewReadOnlyClaudeProposer returns a Claude fork-session proposer restricted to
// read-only tools. sessionID is Task.SessionID; cwd is the task worktree.
func NewReadOnlyClaudeProposer(sessionID, cwd string) Proposer {
	return reviewClaude.NewProposer(sessionID, cwd, reviewClaude.WithProposerReadOnly())
}
