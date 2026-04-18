package agents

import (
	"time"

	"changkun.de/x/wallfacer/internal/store"
)

// Implementation is the heavyweight-tier descriptor driving each
// turn of the task-execution loop. Mounts task worktrees read-write
// + the board manifest + sibling worktrees, runs the agent as a
// multi-turn conversation (the runner's turn loop handles session
// recovery and auto-continue), and returns the raw Output untouched
// so the caller can read every NDJSON field (SessionID, StopReason,
// IsError) directly.
//
// ParseResult returns *Output — the raw agent output.
var Implementation = Role{
	Activity:    store.SandboxActivityImplementation,
	Name:        "impl",
	Description: "Executes the task prompt and produces commits on the task's worktree.",
	Timeout: func(t *store.Task) time.Duration {
		if t == nil {
			return 0
		}
		return time.Duration(t.Timeout) * time.Minute
	},
	MountMode:   MountReadWrite,
	MountBoard:  true,
	SingleTurn:  false,
	ParseResult: PassthroughParse,
}

// Testing is the heavyweight-tier descriptor for the test-verification
// agent. Same mount profile as Implementation; differs only in its
// activity tag and the sandbox-routing bucket that tag selects.
//
// ParseResult returns *Output — the raw agent output.
var Testing = Role{
	Activity:    store.SandboxActivityTesting,
	Name:        "test",
	Description: "Runs the task's test suite and classifies the verdict.",
	Timeout: func(t *store.Task) time.Duration {
		if t == nil {
			return 0
		}
		return time.Duration(t.Timeout) * time.Minute
	},
	MountMode:   MountReadWrite,
	MountBoard:  true,
	SingleTurn:  false,
	ParseResult: PassthroughParse,
}

// PassthroughParse hands the raw *Output back to the caller. The
// heavyweight roles use this because the turn loop consumes every
// field of Output directly and would re-pack a typed result just to
// unpack it again.
func PassthroughParse(o *Output) (any, error) { return o, nil }
