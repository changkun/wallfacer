package handler

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/pkg/cmdexec"
	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/spec"
	"latere.ai/x/wallfacer/internal/store"
)

// runnerDriftTester adapts the runner's one-shot drift agent to the DriftTester
// interface the completion hook consumes.
type runnerDriftTester struct{ r runner.Interface }

// NewRunnerDriftTester wraps the runner as a DriftTester. Returns nil for a nil
// runner so the hook falls back to complete-on-done.
func NewRunnerDriftTester(r runner.Interface) DriftTester {
	if r == nil {
		return nil
	}
	return runnerDriftTester{r: r}
}

func (t runnerDriftTester) AssessDrift(ctx context.Context, in DriftTestInput) (spec.DriftVerdict, error) {
	return t.r.AssessDrift(ctx, in.SpecBody, in.Affects, in.ChangedFiles, in.Diff)
}

// DriftTestInput is the context handed to the drift tester: the spec it is
// judging plus the task's actual changes.
type DriftTestInput struct {
	SpecPath     string
	SpecBody     string
	Affects      []string
	Diff         string
	ChangedFiles []string
}

// DriftTester assesses how far a task's implementation drifted from its spec.
// The production implementation runs an agent; tests inject a stub. Wiring it
// is gated behind WALLFACER_DRIFT_TESTER (see driftTesterEnabled).
type DriftTester interface {
	AssessDrift(ctx context.Context, in DriftTestInput) (spec.DriftVerdict, error)
}

// driftTesterEnabled reports whether the task-done drift pipeline is on. Off by
// default; the hook then preserves historical behavior (write complete on task
// done). Enabled per OQ2 once the pipeline is stable.
func driftTesterEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("WALLFACER_DRIFT_TESTER")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

// SpecCompletionHook returns a callback for store.OnDone that moves the source
// spec through the lifecycle when its dispatched task finishes.
//
// With the drift pipeline off (tester nil or enabled() false — the default), it
// writes complete directly, preserving the historical behavior. With it on, it
// runs the task-done drift pipeline: validated → testing, tester verdict,
// branch to complete/stale, and fan out staleness to dependents.
func SpecCompletionHook(workspaceFn func() []string, tester DriftTester, enabled func() bool) func(store.Task) {
	return func(task store.Task) {
		if task.SpecSourcePath == "" {
			return
		}
		workspaces := workspaceFn()
		absPath := findSpecFile(workspaces, task.SpecSourcePath)
		if absPath == "" {
			logger.Store.Warn("spec completion hook: spec file not found",
				"task", task.ID, "spec", task.SpecSourcePath)
			return
		}

		if enabled == nil {
			enabled = driftTesterEnabled
		}
		if tester == nil || !enabled() {
			writeSpecComplete(task, absPath)
			return
		}
		runDriftPipeline(context.Background(), task, absPath, workspaces, tester)
	}
}

// writeSpecComplete is the historical task-done behavior: mark the spec
// complete unconditionally.
func writeSpecComplete(task store.Task, absPath string) {
	if err := spec.UpdateFrontmatter(absPath, map[string]any{
		"status":  string(spec.StatusComplete),
		"updated": time.Now(),
	}); err != nil {
		logger.Store.Error("spec completion hook: failed to update frontmatter",
			"task", task.ID, "spec", task.SpecSourcePath, "error", err)
		return
	}
	logger.Store.Info("spec completion hook: marked spec complete",
		"task", task.ID, "spec", task.SpecSourcePath)
}

// runDriftPipeline runs the task-done drift assessment for a completed task.
func runDriftPipeline(ctx context.Context, task store.Task, absPath string, workspaces []string, tester DriftTester) {
	relPath := task.SpecSourcePath
	s, err := spec.ParseFile(absPath)
	if err != nil {
		logger.Store.Error("drift pipeline: parse spec", "spec", relPath, "error", err)
		return
	}
	// Only a validated spec enters testing. If it went stale mid-implementation
	// (an upstream changed), leave it alone — do not run the tester.
	if s.Status != spec.StatusValidated {
		logger.Store.Warn("drift pipeline: spec not validated, skipping",
			"spec", relPath, "status", s.Status)
		return
	}

	ws := findWorkspaceRoot(workspaces, absPath)
	base, tip := taskCommitRange(task, ws)

	// 1. Enter testing, recording the implementation commit range.
	enter := map[string]any{
		"status":  string(spec.StatusTesting),
		"updated": time.Now(),
	}
	if base != "" && tip != "" {
		enter["implementation_commit"] = base + ".." + tip
	}
	if err := spec.UpdateFrontmatter(absPath, enter); err != nil {
		logger.Store.Error("drift pipeline: enter testing", "spec", relPath, "error", err)
		return
	}
	if err := commitSpecChanges(ctx, workspaces, absPath, []string{relPath},
		fmt.Sprintf("%s: enter testing", relPath)); err != nil {
		logger.Store.Warn("drift pipeline: commit enter-testing", "spec", relPath, "error", err)
	}

	// 2. Run the tester on the spec body + actual diff.
	changed := gitDiffNames(ctx, ws, base, tip)
	verdict, terr := tester.AssessDrift(ctx, DriftTestInput{
		SpecPath:     relPath,
		SpecBody:     s.Body,
		Affects:      s.Affects,
		Diff:         gitDiff(ctx, ws, base, tip),
		ChangedFiles: changed,
	})
	if terr != nil {
		// Never silently fall back to complete. Hold at testing with a reason.
		reason := fmt.Sprintf("tester failed at %s: %v", time.Now().Format(time.RFC3339), terr)
		if err := spec.UpdateFrontmatter(absPath, map[string]any{
			"testing_pending": reason,
			"updated":         time.Now(),
		}); err != nil {
			logger.Store.Error("drift pipeline: write testing_pending", "spec", relPath, "error", err)
			return
		}
		if err := commitSpecChanges(ctx, workspaces, absPath, []string{relPath},
			fmt.Sprintf("%s: tester failed, awaiting retry", relPath)); err != nil {
			logger.Store.Warn("drift pipeline: commit testing_pending", "spec", relPath, "error", err)
		}
		logger.Store.Warn("drift pipeline: tester failed", "spec", relPath, "error", terr)
		return
	}

	// 3. Classify server-side and branch.
	level := spec.ClassifyDrift(verdict)
	status, fanOut := spec.DriftOutcome(level)

	// 4. Write the verdict: status, clear testing markers, append the Outcome.
	if err := spec.UpdateFrontmatter(absPath, map[string]any{
		"status":                string(status),
		"implementation_commit": nil,
		"testing_pending":       nil,
		"updated":               time.Now(),
	}); err != nil {
		logger.Store.Error("drift pipeline: write verdict", "spec", relPath, "error", err)
		return
	}
	if err := spec.SetOutcome(absPath, formatOutcome(level, verdict)); err != nil {
		logger.Store.Warn("drift pipeline: write outcome section", "spec", relPath, "error", err)
	}

	// 5. Fan out staleness to dependents (moderate/significant), staging the
	// cascade into the same commit as the verdict so git revert reverses both.
	relPaths := []string{relPath}
	if fanOut {
		if tree, err := spec.BuildTree(filepath.Join(ws, "specs")); err == nil {
			impacted := unionPaths(
				spec.DependsOnImpact(tree, relPath),
				spec.AffectsImpactFromDiff(tree, changed, relPath),
			)
			resolve := func(p string) string { return filepath.Join(ws, filepath.FromSlash(p)) }
			applied, ferr := spec.FanOutStale(tree, impacted, resolve, time.Now())
			if ferr != nil {
				logger.Store.Warn("drift pipeline: fan-out", "spec", relPath, "error", ferr)
			}
			relPaths = append(relPaths, applied...)
		}
	}

	subject := fmt.Sprintf("%s: mark %s", relPath, status)
	if status == spec.StatusStale {
		subject = fmt.Sprintf("%s: mark stale (drift: %s)", relPath, level)
	}
	if err := commitSpecChanges(ctx, workspaces, absPath, relPaths, subject); err != nil {
		logger.Store.Warn("drift pipeline: commit verdict", "spec", relPath, "error", err)
	}
	logger.Store.Info("drift pipeline: verdict applied",
		"spec", relPath, "drift", level, "status", status, "fanout", len(relPaths)-1)
}

// taskCommitRange resolves the base..tip commit range for the spec's workspace
// from the task's recorded commit hashes. Falls back to any single recorded
// repo when the workspace key does not match. Returns empty strings when no
// range is available.
func taskCommitRange(task store.Task, ws string) (base, tip string) {
	pick := func(m map[string]string) string {
		if m == nil {
			return ""
		}
		if v, ok := m[ws]; ok {
			return v
		}
		for _, v := range m {
			return v // single-repo task: take the only entry
		}
		return ""
	}
	return pick(task.BaseCommitHashes), pick(task.CommitHashes)
}

func gitDiff(ctx context.Context, ws, base, tip string) string {
	if ws == "" || base == "" || tip == "" {
		return ""
	}
	out, err := cmdexec.Git(ws, "diff", base+".."+tip).WithContext(ctx).Output()
	if err != nil {
		return ""
	}
	return out
}

func gitDiffNames(ctx context.Context, ws, base, tip string) []string {
	if ws == "" || base == "" || tip == "" {
		return nil
	}
	out, err := cmdexec.Git(ws, "diff", "--name-only", base+".."+tip).WithContext(ctx).Output()
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if p := strings.TrimSpace(line); p != "" {
			files = append(files, p)
		}
	}
	return files
}

func unionPaths(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	var out []string
	for _, p := range append(append([]string{}, a...), b...) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// formatOutcome renders the drift verdict as the spec body's Outcome section.
func formatOutcome(level spec.DriftLevel, v spec.DriftVerdict) string {
	var b strings.Builder
	fmt.Fprintf(&b, "**Drift: %s** (%s)\n\n", level, time.Now().Format(time.DateOnly))
	if len(v.Unexpected) > 0 {
		fmt.Fprintf(&b, "- Unexpected files: %s\n", strings.Join(v.Unexpected, ", "))
	}
	if len(v.Missing) > 0 {
		fmt.Fprintf(&b, "- Missing files: %s\n", strings.Join(v.Missing, ", "))
	}
	if v.Criteria.Total > 0 {
		fmt.Fprintf(&b, "- Criteria: %d/%d satisfied, %d diverged\n",
			v.Criteria.Satisfied, v.Criteria.Total, v.Criteria.Diverged)
	}
	if s := strings.TrimSpace(v.Summary); s != "" {
		fmt.Fprintf(&b, "\n%s\n", s)
	}
	return b.String()
}
