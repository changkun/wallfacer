package runner

import (
	"time"

	"changkun.de/x/wallfacer/internal/agents"
	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
)

// mountMode enumerates the three container-mount profiles the runner
// uses to dispatch agent roles. Kept unexported because it is a pure
// runner-plumbing concern: the agents package exposes high-level
// capabilities instead.
type mountMode int

const (
	mountNone mountMode = iota
	mountReadOnly
	mountReadWrite
)

// agentBinding is the runner-private dispatch info for one agent
// role. Every field is orchestration detail that does not belong on
// the public agents.Role descriptor: which sandbox-routing activity
// bucket the role maps to, what container-mount profile it needs,
// whether the turn loop drives it, how to parse its output, and an
// optional per-role model resolver.
type agentBinding struct {
	Activity    store.SandboxActivity
	Timeout     func(*store.Task) time.Duration
	MountMode   mountMode
	MountBoard  bool
	SingleTurn  bool
	ParseResult func(*agentOutput) (any, error)
	Model       func(sandbox.Type) string
}

// agentBindings keys the private per-slug dispatch info off the
// Role.Slug values declared in internal/agents. New agent slugs added
// to BuiltinAgents without a matching entry here will fail at lookup
// time — runAgent surfaces a clear error rather than running with
// zero-value fields.
var agentBindings = map[string]agentBinding{
	agents.Title.Slug: {
		Activity:    store.SandboxActivityTitle,
		Timeout:     func(*store.Task) time.Duration { return constants.TitleAgentTimeout },
		MountMode:   mountNone,
		SingleTurn:  true,
		ParseResult: parseTitleResult,
	},
	agents.Oversight.Slug: {
		Activity:    store.SandboxActivityOversight,
		Timeout:     func(*store.Task) time.Duration { return constants.OversightAgentTimeout },
		MountMode:   mountNone,
		SingleTurn:  true,
		ParseResult: parseOversightAgentResult,
	},
	agents.CommitMessage.Slug: {
		Activity:    store.SandboxActivityCommitMessage,
		Timeout:     func(*store.Task) time.Duration { return constants.CommitMessageAgentTimeout },
		MountMode:   mountNone,
		SingleTurn:  true,
		ParseResult: parseCommitMessageResult,
	},
	agents.IdeaAgent.Slug: {
		Activity: store.SandboxActivityIdeaAgent,
		// Ideation caller wraps the call in its own deadline derived
		// from the task's Timeout field; no role-level timeout.
		Timeout:     nil,
		MountMode:   mountReadOnly,
		SingleTurn:  true,
		ParseResult: rawResultParse,
	},
	agents.Implementation.Slug: {
		Activity: store.SandboxActivityImplementation,
		Timeout: func(t *store.Task) time.Duration {
			if t == nil {
				return 0
			}
			return time.Duration(t.Timeout) * time.Minute
		},
		MountMode:   mountReadWrite,
		MountBoard:  true,
		SingleTurn:  false,
		ParseResult: passthroughParse,
	},
	agents.Testing.Slug: {
		Activity: store.SandboxActivityTesting,
		Timeout: func(t *store.Task) time.Duration {
			if t == nil {
				return 0
			}
			return time.Duration(t.Timeout) * time.Minute
		},
		MountMode:   mountReadWrite,
		MountBoard:  true,
		SingleTurn:  false,
		ParseResult: passthroughParse,
	},
}

// bindingFor looks up the runner-side dispatch plumbing for an agent
// slug. Returns zero-value + false when the slug is unknown so
// runAgent can surface a "no binding registered" error.
func bindingFor(slug string) (agentBinding, bool) {
	b, ok := agentBindings[slug]
	return b, ok
}

// rawResultParse hands the raw result string back unchanged. Used by
// roles whose downstream caller does the role-specific parsing (ideation).
func rawResultParse(o *agentOutput) (any, error) { return o.Result, nil }

// passthroughParse hands the raw *agentOutput back to the caller.
// Heavyweight roles use this because the turn loop consumes every
// field of agentOutput directly.
func passthroughParse(o *agentOutput) (any, error) { return o, nil }
