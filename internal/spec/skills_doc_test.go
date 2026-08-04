package spec

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The committed .claude/skills/ copy is what `claude -p` auto-discovers,
// so a dispatched harness task follows whatever those files say. They
// spell out the same vocabulary the code owns — the status set and the
// transition actions — with nothing binding the two. These tests bind
// them.

// readSkills returns every committed skill body keyed by its skill name.
func readSkills(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join(repoRootDir(t), ".claude", "skills")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read .claude/skills: %v", err)
	}
	out := map[string]string{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(root, e.Name(), "skill.md"))
		if err != nil {
			t.Errorf("skill %s has no skill.md: %v", e.Name(), err)
			continue
		}
		out[e.Name()] = string(b)
	}
	if len(out) == 0 {
		t.Fatal("no skills under .claude/skills")
	}
	return out
}

// enumSeparator matches the connector between two status literals that
// belong to the same enumeration — "a/b", "a|b", "a, b", "a, or b",
// "a or b". Padded pipes (" | ") are deliberately excluded: those are
// markdown table cells, where the neighbouring statuses are a row's
// source and its allowed targets, not one list.
var enumSeparator = regexp.MustCompile(`^(/|\||, ?(or )?| or )$`)

var whitespaceRun = regexp.MustCompile(`\s+`)

func skillStatusRuns(text string) [][]string {
	// These files are hard-wrapped, so an enumeration routinely spans a
	// line break. Collapse whitespace so the wrap does not end a run.
	plain := strings.NewReplacer("`", "", "**", "", "*", "").Replace(text)
	plain = whitespaceRun.ReplaceAllString(plain, " ")

	names := make([]string, 0, len(ValidStatuses()))
	for _, s := range ValidStatuses() {
		names = append(names, regexp.QuoteMeta(string(s)))
	}
	re := regexp.MustCompile(`\b(` + strings.Join(names, "|") + `)\b`)

	var runs [][]string
	var cur []string
	prevEnd := -1
	for _, loc := range re.FindAllStringIndex(plain, -1) {
		if cur != nil && enumSeparator.MatchString(plain[prevEnd:loc[0]]) {
			cur = append(cur, plain[loc[0]:loc[1]])
		} else {
			if len(cur) > 0 {
				runs = append(runs, cur)
			}
			cur = []string{plain[loc[0]:loc[1]]}
		}
		prevEnd = loc[1]
	}
	if len(cur) > 0 {
		runs = append(runs, cur)
	}
	return runs
}

// TestSkillStatusEnumerationsAreComplete asserts a skill that spells out
// the status vocabulary spells out all of it. A run counts as the
// vocabulary only when it includes `vague`: the initial state is never a
// transition target, so any other run of statuses is a list of allowed
// targets rather than the full set.
func TestSkillStatusEnumerationsAreComplete(t *testing.T) {
	for name, body := range readSkills(t) {
		for _, run := range skillStatusRuns(body) {
			got := map[string]bool{}
			for _, s := range run {
				got[s] = true
			}
			if len(run) < 3 || !got[string(StatusVague)] {
				continue
			}
			for _, want := range ValidStatuses() {
				if !got[string(want)] {
					t.Errorf("%s enumerates statuses %v but omits %q; the vocabulary is %v",
						name, run, want, ValidStatuses())
				}
			}
		}
	}
}

// TestSkillsDoNotClaimIllegalEdges asserts that every "from → to" pair a
// skill writes is a legal edge. The skills use the arrow to describe
// transitions they intend to drive, so an illegal pair is an instruction
// the server will reject with 422.
func TestSkillsDoNotClaimIllegalEdges(t *testing.T) {
	names := make([]string, 0, len(ValidStatuses()))
	for _, s := range ValidStatuses() {
		names = append(names, regexp.QuoteMeta(string(s)))
	}
	group := `(` + strings.Join(names, "|") + `)`
	arrow := regexp.MustCompile(`\b` + group + "`?" + ` ?(?:→|->) ?` + "`?" + `\b` + group + `\b`)

	// Prose that names an illegal edge in order to forbid it. Matching on
	// the surrounding sentence would be fragile, so the skills mark these
	// with "never", "illegal", "forbids", or "not a legal edge" nearby.
	forbidding := regexp.MustCompile(`(?i)(never|illegal|forbid|not a legal|no longer|do not|don't)`)

	for name, body := range readSkills(t) {
		plain := strings.NewReplacer("`", "", "**", "").Replace(body)
		lines := strings.Split(plain, "\n")
		for i, line := range lines {
			// The forbidding word often lands on the neighbouring line
			// because these files are hard-wrapped, so judge a window.
			window := strings.Join(lines[max(0, i-1):min(len(lines), i+2)], " ")
			for _, m := range arrow.FindAllStringSubmatch(line, -1) {
				from, to := Status(m[1]), Status(m[2])
				if StatusMachine.CanTransition(from, to) || forbidding.MatchString(window) {
					continue
				}
				t.Errorf("%s writes %s → %s, which StatusMachine does not allow: %q",
					name, from, to, strings.TrimSpace(line))
			}
		}
	}
}

// TestSkillTransitionActionsMatchHandler asserts the action list the
// skills advertise matches the switch in SpecTransition. A skill naming
// an action the handler dropped sends the agent into a 400.
func TestSkillTransitionActionsMatchHandler(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRootDir(t), "internal", "handler", "specs_dispatch.go"))
	if err != nil {
		t.Fatalf("read specs_dispatch.go: %v", err)
	}
	caseRe := regexp.MustCompile(`(?m)^\tcase "([a-z-]+)":`)
	want := map[string]bool{}
	for _, m := range caseRe.FindAllStringSubmatch(string(src), -1) {
		want[m[1]] = true
	}
	if len(want) == 0 {
		t.Fatal("no transition actions found in specs_dispatch.go")
	}

	// wf-spec-drive is the skill that documents the full action list.
	body, ok := readSkills(t)["wf-spec-drive"]
	if !ok {
		t.Skip("wf-spec-drive not vendored")
	}
	start := strings.Index(body, "Actions:")
	if start < 0 {
		t.Fatal("wf-spec-drive no longer lists the transition actions")
	}
	end := strings.Index(body[start:], ".\n")
	if end < 0 {
		end = len(body) - start
	}
	listed := body[start : start+end]

	for action := range want {
		if !strings.Contains(listed, "`"+action+"`") {
			t.Errorf("wf-spec-drive omits the transition action %q", action)
		}
	}
}
