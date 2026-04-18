package agents

import (
	"fmt"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/store"
)

// Title is the headless-tier descriptor for the title-generation
// sub-agent. Emits a 2–5 word summary of the task prompt; no
// mounts, single-turn, and uses the title-specific CLAUDE_TITLE_MODEL
// env var (bound by the runner at call time via Role.Model) so
// operators can route title work to a cheaper model.
//
// ParseResult returns string — the trimmed title.
var Title = Role{
	Activity:    store.SandboxActivityTitle,
	Name:        "title",
	Description: "Generates a short 2–5 word summary of a task's goal.",
	Timeout:     func(*store.Task) time.Duration { return constants.TitleAgentTimeout },
	MountMode:   MountNone,
	SingleTurn:  true,
	ParseResult: parseTitleResult,
}

// Oversight is the headless-tier descriptor for the oversight-summary
// sub-agent. Parses the post-run event timeline and returns a typed
// []store.OversightPhase via ParseOversightResult; callers then fill
// in missing phase timestamps from the real activity log.
//
// The oversight agent is reused for both regular-run oversight and
// test-run oversight — callers pass an ActivityOverride on the
// runAgent opts to split usage accounting across the two activities
// without a separate descriptor.
//
// ParseResult returns []store.OversightPhase.
var Oversight = Role{
	Activity:    store.SandboxActivityOversight,
	Name:        "oversight",
	Description: "Summarises an agent run's activity into a structured phase list.",
	Timeout:     func(*store.Task) time.Duration { return constants.OversightAgentTimeout },
	MountMode:   MountNone,
	SingleTurn:  true,
	ParseResult: parseOversightResult,
}

// CommitMessage is the headless-tier descriptor for the commit-message
// generation sub-agent. Returns the agent's message string, already
// trimmed of surrounding whitespace and backticks (the two quoting
// patterns Claude consistently emits).
//
// ParseResult returns string — the trimmed commit message.
var CommitMessage = Role{
	Activity:    store.SandboxActivityCommitMessage,
	Name:        "commit-msg",
	Description: "Produces a descriptive git commit message from the task prompt and diff.",
	Timeout:     func(*store.Task) time.Duration { return constants.CommitMessageAgentTimeout },
	MountMode:   MountNone,
	SingleTurn:  true,
	ParseResult: parseCommitMessageResult,
}

// parseTitleResult extracts the trimmed title string from an Output.
// The agent returns the title verbatim in `result`; we strip
// surrounding quotes and whitespace before returning.
func parseTitleResult(o *Output) (any, error) {
	title := strings.TrimSpace(o.Result)
	title = strings.Trim(title, `"'`)
	return strings.TrimSpace(title), nil
}

// parseOversightResult is declared in oversight_parser.go alongside
// ParseOversightPhaseList so the phase-parsing logic stays colocated
// with the phase type.
//
// The runner's adapter then fills in missing phase timestamps from
// the real activity log after runAgent returns.

// parseCommitMessageResult trims the commit-message agent output and
// surfaces a typed error when the agent returned an error result or
// an empty string. Callers unwrap the string via type assertion.
func parseCommitMessageResult(o *Output) (any, error) {
	if o.IsError {
		msg := strings.TrimSpace(o.Result)
		if msg == "" {
			msg = "agent returned an error result"
		}
		return "", fmt.Errorf("%s", msg)
	}
	msg := strings.TrimSpace(o.Result)
	msg = strings.Trim(msg, "`")
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", fmt.Errorf("blank result")
	}
	return msg, nil
}
