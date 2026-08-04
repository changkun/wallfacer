package spec

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// docPath returns docs/internals/plan-mode.md, which documents the
// lifecycle twice — once as a mermaid state diagram, once as a table.
// Both are prose the compiler never checks, so they drift silently when
// StatusMachine gains a state or an edge.
func docPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRootDir(t), "docs", "internals", "plan-mode.md")
}

// repoRootDir walks up from this source file to the repository root.
func repoRootDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

func readDoc(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(docPath(t))
	if err != nil {
		t.Fatalf("read plan-mode.md: %v", err)
	}
	return string(b)
}

// wantEdges renders StatusMachine as "from → to" strings.
func wantEdges() map[string]bool {
	out := map[string]bool{}
	for _, from := range ValidStatuses() {
		for _, to := range StatusMachine.Allowed(from) {
			out[string(from)+" → "+string(to)] = true
		}
	}
	return out
}

// compareEdges reports edges the doc invents and edges it omits.
func compareEdges(t *testing.T, source string, got map[string]bool) {
	t.Helper()
	want := wantEdges()
	for e := range got {
		if !want[e] {
			t.Errorf("%s documents %s, which StatusMachine does not allow", source, e)
		}
	}
	for e := range want {
		if !got[e] {
			t.Errorf("%s omits the legal edge %s", source, e)
		}
	}
}

var mermaidEdge = regexp.MustCompile(`(?m)^\s*(\w+)\s*-->\s*(\w+)\s*$`)

// TestPlanModeDocMermaidMatchesStateMachine checks the state diagram.
func TestPlanModeDocMermaidMatchesStateMachine(t *testing.T) {
	doc := readDoc(t)
	start := strings.Index(doc, "stateDiagram-v2")
	if start < 0 {
		t.Fatal("no stateDiagram-v2 block in plan-mode.md")
	}
	end := strings.Index(doc[start:], "```")
	if end < 0 {
		t.Fatal("unterminated mermaid block")
	}

	got := map[string]bool{}
	for _, m := range mermaidEdge.FindAllStringSubmatch(doc[start:start+end], -1) {
		got[m[1]+" → "+m[2]] = true
	}
	if len(got) == 0 {
		t.Fatal("state diagram has no edges")
	}
	compareEdges(t, "the plan-mode.md state diagram", got)
}

// TestPlanModeDocTableMatchesStateMachine checks the From/Allowed
// Targets table that follows the diagram.
func TestPlanModeDocTableMatchesStateMachine(t *testing.T) {
	doc := readDoc(t)
	start := strings.Index(doc, "| From ")
	if start < 0 {
		t.Fatal("no lifecycle transition table in plan-mode.md")
	}

	valid := map[string]bool{}
	for _, s := range ValidStatuses() {
		valid[string(s)] = true
	}

	got := map[string]bool{}
	lines := strings.Split(doc[start:], "\n")
	for _, line := range lines[1:] {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			break
		}
		cols := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cols) < 2 {
			continue
		}
		from := strings.TrimSpace(cols[0])
		if !valid[from] {
			continue // separator row
		}
		for to := range strings.SplitSeq(cols[1], ",") {
			got[from+" → "+strings.TrimSpace(to)] = true
		}
	}
	if len(got) == 0 {
		t.Fatal("lifecycle transition table has no rows")
	}
	compareEdges(t, "the plan-mode.md transition table", got)
}

// TestPlanModeDocStatusCommentIsComplete checks the inline `// vague |
// drafted | ...` comment on the Spec struct snippet, the third place the
// vocabulary is spelled out.
func TestPlanModeDocStatusCommentIsComplete(t *testing.T) {
	doc := readDoc(t)
	re := regexp.MustCompile(`Status\s+Status\s+//\s*([a-z |]+)`)
	m := re.FindStringSubmatch(doc)
	if m == nil {
		t.Fatal("no Status field comment in the plan-mode.md struct snippet")
	}

	var got []string
	for s := range strings.SplitSeq(m[1], "|") {
		if s = strings.TrimSpace(s); s != "" {
			got = append(got, s)
		}
	}
	for _, s := range ValidStatuses() {
		if !slices.Contains(got, string(s)) {
			t.Errorf("the plan-mode.md Status comment lists %v but omits %q", got, s)
		}
	}
}
