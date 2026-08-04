package agentsession

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/spec"
)

// The slash-command templates are prose, so the compiler cannot catch it
// when the lifecycle in internal/spec grows a state or an edge and the
// prose keeps describing the old model. These tests pin the prose to the
// code: the status vocabulary comes from [spec.ValidStatuses] and the
// set of hand-writable states comes from [spec.StatusMachine].

// readTemplates returns every embedded template keyed by file name.
func readTemplates(t *testing.T) map[string]string {
	t.Helper()
	entries, err := fs.ReadDir(commandTemplatesFS, "commands_templates")
	if err != nil {
		t.Fatalf("read template dir: %v", err)
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		b, err := fs.ReadFile(commandTemplatesFS, "commands_templates/"+e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		out[e.Name()] = string(b)
	}
	if len(out) == 0 {
		t.Fatal("no templates embedded")
	}
	return out
}

// statusRunSeparator matches the connector text allowed between two
// status literals that belong to the same enumeration ("a/b", "a, b",
// "a, or b"). Anything else ends the run.
var statusRunSeparator = regexp.MustCompile(`^\s*(/|,\s*|,?\s*(or|and)\s+)$`)

// statusRuns finds every enumeration of status literals in text — a run
// of status words joined only by enumeration separators. A run of one or
// two is a passing mention ("vague/drafted specs"); three or more is the
// template spelling out the vocabulary.
func statusRuns(text string) [][]string {
	names := make([]string, 0, len(spec.ValidStatuses()))
	for _, s := range spec.ValidStatuses() {
		names = append(names, regexp.QuoteMeta(string(s)))
	}
	re := regexp.MustCompile(`\b(` + strings.Join(names, "|") + `)\b`)

	locs := re.FindAllStringIndex(text, -1)
	var runs [][]string
	var cur []string
	prevEnd := -1
	for _, loc := range locs {
		if cur != nil && statusRunSeparator.MatchString(text[prevEnd:loc[0]]) {
			cur = append(cur, text[loc[0]:loc[1]])
		} else {
			if len(cur) > 0 {
				runs = append(runs, cur)
			}
			cur = []string{text[loc[0]:loc[1]]}
		}
		prevEnd = loc[1]
	}
	if len(cur) > 0 {
		runs = append(runs, cur)
	}
	return runs
}

// TestTemplateStatusEnumerationsAreComplete asserts that a template
// listing the status vocabulary lists all of it. Partial lists teach the
// agent a lifecycle the server does not implement.
func TestTemplateStatusEnumerationsAreComplete(t *testing.T) {
	want := make(map[string]bool, len(spec.ValidStatuses()))
	for _, s := range spec.ValidStatuses() {
		want[string(s)] = true
	}

	for name, body := range readTemplates(t) {
		for _, run := range statusRuns(body) {
			if len(run) < 3 {
				continue // a passing mention, not an enumeration
			}
			got := make(map[string]bool, len(run))
			for _, s := range run {
				got[s] = true
			}
			for s := range want {
				if !got[s] {
					t.Errorf("%s enumerates statuses %v but omits %q; the vocabulary is %v",
						name, run, s, spec.ValidStatuses())
				}
			}
		}
	}
}

// setStatusInstruction matches prose telling the agent to write a status
// value into the spec by hand.
var setStatusInstruction = regexp.MustCompile(`(?i)set\s+(?:the\s+)?(?:spec\s+)?status\s+to\s+"?([a-z-]+)"?`)

// TestTemplatesDoNotHandWriteServerOwnedStatuses asserts no template
// instructs a direct write to a status the agent cannot legally reach by
// editing frontmatter. `testing` and `complete` are entered only by the
// server's drift pipeline or the `force-complete` transition action, so
// prose that says "set status to complete" walks the illegal
// validated → complete edge.
func TestTemplatesDoNotHandWriteServerOwnedStatuses(t *testing.T) {
	serverOwned := map[spec.Status]bool{
		spec.StatusTesting:  true,
		spec.StatusComplete: true,
	}
	// Guard the premise: a legal edge into these states must not exist
	// from validated, or this test is asserting the wrong thing.
	for target := range serverOwned {
		if spec.StatusMachine.CanTransition(spec.StatusValidated, target) &&
			target == spec.StatusComplete {
			t.Fatalf("validated → %s is now legal; revisit this test", target)
		}
	}

	for name, body := range readTemplates(t) {
		for _, m := range setStatusInstruction.FindAllStringSubmatch(body, -1) {
			if serverOwned[spec.Status(m[1])] {
				t.Errorf("%s instructs %q, but %q is reached through the server transition API, not a frontmatter edit",
					name, strings.TrimSpace(m[0]), m[1])
			}
		}
	}
}

// setDispatchedTaskID matches prose telling the agent to write the
// dispatched_task_id frontmatter field itself.
var setDispatchedTaskID = regexp.MustCompile(`(?i)set\s+(?:the\s+)?` + "`?" + `dispatched_task_id` + "`?" + `\s+to`)

// TestTemplatesDoNotHandWriteDispatchedTaskID asserts no template tells
// the agent to write dispatched_task_id. The dispatch transition
// (handler.DispatchSpecs) creates the task, writes the link, and commits
// in one step; a hand-written value — a placeholder UUID especially —
// points the spec at a task that does not exist.
func TestTemplatesDoNotHandWriteDispatchedTaskID(t *testing.T) {
	for name, body := range readTemplates(t) {
		if m := setDispatchedTaskID.FindString(body); m != "" {
			t.Errorf("%s instructs %q, but the dispatch transition owns that field", name, strings.TrimSpace(m))
		}
	}
}
