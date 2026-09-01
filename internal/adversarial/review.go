// Package adversarial wires wallfacer's harnesses into the topos adversarial
// debate protocol. It provides implementations of [Verifier] — the
// wallfacer-side verification interface — backed by wallfacer's runner.
//
// This package is the single wallfacer seam onto the topos adversarial engine
// (latere.ai/x/topos/adversarial and its claude proposer): it holds the engine
// implementations, so it names the engine, and the embeddable-boundary guard
// (internal/agentgraph) permits those imports only here. Every other wallfacer
// package depends on this package's own types instead.
//
// [ReviewVerifier] is the only implementation. It is constructed once at
// handler startup; the reviewEnabled runtime flag is checked per call rather
// than by swapping the implementation, and verification additionally requires
// the task to have a non-nil SessionID.
package adversarial

import (
	"context"
	"fmt"
	"strings"

	toposadv "latere.ai/x/topos/adversarial"

	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/runner"
)

// ReviewVerifier implements [Verifier] using the topos adversarial Engine.
// It wires a Claude SessionProposer (fork-session) and a HarnessCritic
// (wallfacer runner one-shot) into the engine and calls Run.
type ReviewVerifier struct {
	runner          runner.Interface
	criticHarnesses []harness.ID // rotated per fork for perspective diversity
}

// NewReviewVerifier creates a verifier whose critics rotate across the given
// harnesses by fork index (e.g. Claude on fork 1, Codex on fork 2) for genuine
// perspective diversity — different models with different blind spots, which is
// the point of adversarial debate. Defaults to Claude-only when none are given.
// The proposer is always Claude (fork-session is Claude-native).
func NewReviewVerifier(r runner.Interface, criticHarnesses ...harness.ID) Verifier {
	if len(criticHarnesses) == 0 {
		criticHarnesses = []harness.ID{harness.Claude}
	}
	return &ReviewVerifier{runner: r, criticHarnesses: criticHarnesses}
}

// criticHarnessForFork maps a 1-based fork index onto the configured critic
// harness rotation.
func (v *ReviewVerifier) criticHarnessForFork(forkIdx int) harness.ID {
	n := len(v.criticHarnesses)
	if n == 0 {
		return harness.Claude
	}
	if forkIdx < 1 {
		forkIdx = 1
	}
	return v.criticHarnesses[(forkIdx-1)%n]
}

// Verify runs adversarial verification on a completed task's implementation.
// Returns nil result when SessionID is empty (no fork-session available).
func (v *ReviewVerifier) Verify(ctx context.Context, in VerifyInput) (*VerifyResult, error) {
	if in.SessionID == "" {
		return nil, nil
	}
	proposer := NewSessionProposer(in.SessionID, in.Cwd)

	// Critics run in a throwaway worktree at the task's HEAD, not in.Cwd (the
	// real worktree). wallfacer's claude harness runs agents with
	// --dangerously-skip-permissions, so a critic in the real tree could write
	// to it and run tests that the commit pipeline would stage. Fail the run
	// rather than fall back to the unsafe real-tree path.
	criticCwd, cleanup, err := newCriticWorktree(in.Cwd)
	if err != nil {
		return nil, fmt.Errorf("review: create critic worktree: %w", err)
	}
	defer cleanup()

	engine := &toposadv.Engine{
		StateDir:  in.StateDir,
		Cwd:       in.Cwd,
		ForkCount: in.ForkCount,
		Proposer:  proposer,
		NewCritic: func(forkIdx int) toposadv.Critic {
			return NewHarnessCritic(v.runner, v.criticHarnessForFork(forkIdx), criticCwd)
		},
		MaxRounds:   in.MaxRounds,
		CostCap:     in.CostCapTokens,
		TaskContext: buildTaskContext(in.TaskPrompt, in.Criteria),
		DiffPatch:   in.DiffPatch,
	}

	sum, err := engine.Run(ctx)
	if err != nil {
		return nil, err
	}
	return &VerifyResult{
		Unresolved: sum.Unresolved,
		Headline:   sum.Headline,
		SessionDir: sum.SessionDir,
		USD:        sum.USD,
	}, nil
}

// buildTaskContext combines the task prompt and acceptance criteria into
// the TaskContext field review critics see.
func buildTaskContext(prompt, criteria string) string {
	if criteria == "" {
		return prompt
	}
	return strings.Join([]string{prompt, "## Acceptance Criteria", criteria}, "\n\n")
}
