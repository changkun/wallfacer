package adversarial

import "context"

// Verifier is the wallfacer-side post-run verification interface. The handler
// holds one and calls it after a task finishes; [ReviewVerifier] is the only
// implementation. The type is declared here rather than aliased onto the topos
// engine's Verifier so that the engine's integration surface stays confined to
// this package: a field added or renamed on the engine's own VerifyInput is a
// change to [ReviewVerifier.Verify] only, not to every caller.
type Verifier interface {
	// Verify runs verification on a completed task's implementation. A nil
	// result with a nil error means verification was skipped.
	Verify(ctx context.Context, in VerifyInput) (*VerifyResult, error)
}

// VerifyInput parameterizes one [Verifier.Verify] call.
type VerifyInput struct {
	TaskPrompt    string // task description or intent
	Criteria      string // acceptance criteria; empty means no bar
	SessionID     string // implementation-agent session ID (proposer path)
	DiffPatch     string // pre-computed git diff
	Cwd           string // working directory
	StateDir      string // where sessions/<id>/ is written
	ForkCount     int    // number of critic forks
	MaxRounds     int    // debate rounds per fork; zero uses the engine default
	CostCapTokens int    // soft token budget; zero means unbounded
}

// VerifyResult is returned by a successful [Verifier.Verify] call.
type VerifyResult struct {
	Unresolved int    // open attacks at run end
	Headline   string // markdown claim of highest-contention unresolved attack
	SessionDir string // absolute path to the session folder
	USD        float64
}
