package handler

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/spec"
	"latere.ai/x/wallfacer/internal/store"
)

type stubTester struct {
	verdict spec.DriftVerdict
	err     error
}

func (s stubTester) AssessDrift(_ context.Context, _ DriftTestInput) (spec.DriftVerdict, error) {
	return s.verdict, s.err
}

// runDriftHook seeds a git repo with the given spec files, commits them, and
// fires the completion hook (drift pipeline enabled) on sourceRel.
func runDriftHook(t *testing.T, tester DriftTester, sourceRel string, seed func(ws string)) string {
	t.Helper()
	ws := initPlanningTestRepo(t)
	seed(ws)
	runGit(t, ws, "add", "specs/")
	runGit(t, ws, "commit", "-m", "seed specs")

	hook := SpecCompletionHook(
		func() []string { return []string{ws} },
		tester,
		func() bool { return true },
	)
	hook(store.Task{
		SpecSourcePath:   sourceRel,
		BaseCommitHashes: map[string]string{ws: "HEAD"},
		CommitHashes:     map[string]string{ws: "HEAD"},
	})
	return ws
}

func TestDriftPipeline_MinimalToComplete(t *testing.T) {
	tester := stubTester{verdict: spec.DriftVerdict{
		Criteria: spec.DriftCriteria{Satisfied: 6, Total: 6},
		Summary:  "matches intent",
	}}
	ws := runDriftHook(t, tester, "specs/source.md", func(ws string) {
		writeFanoutSpec(t, ws, "source.md", "validated", "Source", nil, []string{"internal/x/"})
	})

	s, err := spec.ParseFile(filepath.Join(ws, "specs/source.md"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != spec.StatusComplete {
		t.Errorf("status = %q, want complete", s.Status)
	}
	if s.ImplementationCommit != nil || s.TestingPending != nil {
		t.Errorf("testing markers not cleared: impl=%v pending=%v", s.ImplementationCommit, s.TestingPending)
	}
	if !strings.Contains(s.Body, "## Outcome") || !strings.Contains(s.Body, "Drift: minimal") {
		t.Errorf("outcome section missing:\n%s", s.Body)
	}
}

func TestDriftPipeline_SignificantToStaleWithFanout(t *testing.T) {
	// Criteria-absent verdict with missing files → significant → stale + fan-out.
	tester := stubTester{verdict: spec.DriftVerdict{Missing: []string{"core.go"}}}
	ws := runDriftHook(t, tester, "specs/source.md", func(ws string) {
		writeFanoutSpec(t, ws, "source.md", "validated", "Source", nil, []string{"internal/x/"})
		writeFanoutSpec(t, ws, "dep.md", "validated", "Dep", []string{"specs/source.md"}, nil)
	})

	src, _ := spec.ParseFile(filepath.Join(ws, "specs/source.md"))
	if src.Status != spec.StatusStale {
		t.Errorf("source status = %q, want stale", src.Status)
	}
	dep, _ := spec.ParseFile(filepath.Join(ws, "specs/dep.md"))
	if dep.Status != spec.StatusStale {
		t.Errorf("dependent status = %q, want stale (fanned out)", dep.Status)
	}

	// The verdict and the cascade are one commit: reverting it restores both.
	revertHead(t, ws)
	src2, _ := spec.ParseFile(filepath.Join(ws, "specs/source.md"))
	if src2.Status != spec.StatusTesting {
		t.Errorf("after revert, source = %q, want testing", src2.Status)
	}
	dep2, _ := spec.ParseFile(filepath.Join(ws, "specs/dep.md"))
	if dep2.Status != spec.StatusValidated {
		t.Errorf("after revert, dependent = %q, want validated", dep2.Status)
	}
}

func TestDriftPipeline_TesterFailureHoldsAtTesting(t *testing.T) {
	tester := stubTester{err: context.DeadlineExceeded}
	ws := runDriftHook(t, tester, "specs/source.md", func(ws string) {
		writeFanoutSpec(t, ws, "source.md", "validated", "Source", nil, []string{"internal/x/"})
	})

	s, _ := spec.ParseFile(filepath.Join(ws, "specs/source.md"))
	if s.Status != spec.StatusTesting {
		t.Errorf("status = %q, want testing (held on tester failure)", s.Status)
	}
	if s.TestingPending == nil || !strings.Contains(*s.TestingPending, "tester failed") {
		t.Errorf("testing_pending not set: %v", s.TestingPending)
	}
}

func TestDriftPipeline_SkipsNonValidated(t *testing.T) {
	// A spec already stale (e.g. upstream changed mid-implementation) is skipped.
	tester := stubTester{verdict: spec.DriftVerdict{Criteria: spec.DriftCriteria{Satisfied: 1, Total: 1}}}
	ws := runDriftHook(t, tester, "specs/source.md", func(ws string) {
		writeFanoutSpec(t, ws, "source.md", "stale", "Source", nil, []string{"internal/x/"})
	})

	s, _ := spec.ParseFile(filepath.Join(ws, "specs/source.md"))
	if s.Status != spec.StatusStale {
		t.Errorf("status = %q, want stale unchanged (pipeline skipped non-validated)", s.Status)
	}
}

func revertHead(t *testing.T, ws string) {
	t.Helper()
	out, err := exec.Command("git", "-C", ws, "revert", "--no-edit", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("git revert: %v\n%s", err, out)
	}
}
