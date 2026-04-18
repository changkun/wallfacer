package agents

import (
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/store"
)

// Refinement is the inspector-tier descriptor for the refinement
// sub-agent. Reads the workspace read-only and produces a detailed
// spec from the task's current prompt. ParseResult returns the raw
// result string — refine.go's caller cleans it and splits the goal
// / spec sections.
//
// ParseResult returns string — the agent's raw refinement text.
var Refinement = Role{
	Activity:    store.SandboxActivityRefinement,
	Name:        "refine",
	Description: "Expands a task prompt into a detailed implementation spec.",
	Timeout:     func(*store.Task) time.Duration { return constants.RefinementTimeout },
	MountMode:   MountReadOnly,
	SingleTurn:  true,
	ParseResult: rawResultParse,
}

// IdeaAgent is the inspector-tier descriptor used by the ephemeral
// (non-planner) ideation path. The parsed result returned by the
// agent is the raw JSON-embedded list of ideas; the caller unpacks
// it via extractIdeas so the rejection/recovery heuristics live
// close to the ideation-specific parsing logic.
//
// ParseResult returns string — the agent's raw JSON+text blob.
var IdeaAgent = Role{
	Activity:    store.SandboxActivityIdeaAgent,
	Name:        "ideate",
	Description: "Scans the workspace and proposes up to three high-impact task ideas.",
	// Timeout is intentionally nil: the ideation caller wraps the
	// agent in its own deadline derived from the task's Timeout
	// field, which can be user-configurable.
	Timeout:     nil,
	MountMode:   MountReadOnly,
	SingleTurn:  true,
	ParseResult: rawResultParse,
}

// rawResultParse hands the raw result string back unchanged. Used by
// roles whose downstream caller does the role-specific parsing.
func rawResultParse(o *Output) (any, error) { return o.Result, nil }
