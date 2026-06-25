package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestNormalizeAffect(t *testing.T) {
	cases := map[string]string{
		"internal/sandbox/":         "internal/sandbox",
		"internal/sandbox":          "internal/sandbox",
		"internal/foo.go":           "internal/foo.go",
		"a/b/":                      "a/b",
		filepath.FromSlash("a/b/c"): "a/b/c",
	}
	for in, want := range cases {
		if got := normalizeAffect(in); got != want {
			t.Errorf("normalizeAffect(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAffectContainsAndOverlap(t *testing.T) {
	// dir contains path
	if !affectContains("internal/sandbox", "internal/sandbox/backend.go") {
		t.Error("dir should contain nested file")
	}
	if !affectContains("internal/sandbox", "internal/sandbox") {
		t.Error("equality is containment")
	}
	// siblings never contain each other
	if affectContains("internal/sandbox/backend.go", "internal/sandbox/handle.go") {
		t.Error("siblings must not contain each other")
	}
	// prefix-but-not-path-boundary is not containment
	if affectContains("internal/sand", "internal/sandbox/backend.go") {
		t.Error("partial path component must not count as containment")
	}
	// overlap is symmetric
	if !affectsOverlap("internal/sandbox/backend.go", "internal/sandbox") {
		t.Error("overlap should be symmetric (file within dir)")
	}
	if affectsOverlap("internal/runner", "internal/sandbox") {
		t.Error("disjoint dirs must not overlap")
	}
}

func TestDependsOnImpact_SingleHop(t *testing.T) {
	// C depends on B depends on A. An event on A marks only B (direct), not C.
	tree := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusValidated},
		"local/b.md": {Status: StatusValidated, DependsOn: []string{"local/a.md"}},
		"local/c.md": {Status: StatusValidated, DependsOn: []string{"local/b.md"}},
	})
	got := DependsOnImpact(tree, "local/a.md")
	if !slices.Equal(got, []string{"local/b.md"}) {
		t.Errorf("DependsOnImpact(a) = %v, want [local/b.md] (single hop)", got)
	}
}

func TestDependsOnImpact_ArchivedPruned(t *testing.T) {
	// X depends on A but is archived; Y depends only on X. Neither is impacted.
	tree := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusComplete},
		"local/b.md": {Status: StatusValidated, DependsOn: []string{"local/a.md"}},
		"local/x.md": {Status: StatusArchived, DependsOn: []string{"local/a.md"}},
		"local/y.md": {Status: StatusValidated, DependsOn: []string{"local/x.md"}},
	})
	got := DependsOnImpact(tree, "local/a.md")
	if !slices.Equal(got, []string{"local/b.md"}) {
		t.Errorf("DependsOnImpact(a) = %v, want [local/b.md] (archived pruned)", got)
	}
}

func TestAffectsImpactFromDiff(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusValidated, Affects: []string{"internal/runner/"}},
		"local/b.md": {Status: StatusValidated, Affects: []string{"internal/runner/execute.go"}},
		"local/c.md": {Status: StatusValidated, Affects: []string{"internal/runner/container.go"}},
		"local/e.md": {Status: StatusValidated, Affects: []string{"internal/sandbox/"}},
		"local/s.md": {Status: StatusValidated, Affects: []string{"internal/runner/"}}, // source
	})
	changed := []string{"internal/runner/execute.go", "internal/runner/container.go"}
	got := AffectsImpactFromDiff(tree, changed, "local/s.md")
	want := []string{"local/a.md", "local/b.md", "local/c.md"}
	if !slices.Equal(got, want) {
		t.Errorf("AffectsImpactFromDiff = %v, want %v (E untouched, source excluded)", got, want)
	}
}

func TestAffectsImpactFromSpec_Overlap(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/s.md": {Status: StatusValidated, Affects: []string{"internal/runner/"}}, // source
		"local/a.md": {Status: StatusValidated, Affects: []string{"internal/runner/execute.go"}},
		"local/b.md": {Status: StatusValidated, Affects: []string{"internal/runner/"}},
		"local/c.md": {Status: StatusValidated, Affects: []string{"internal/sandbox/"}},
	})
	got := AffectsImpactFromSpec(tree, "local/s.md")
	want := []string{"local/a.md", "local/b.md"}
	if !slices.Equal(got, want) {
		t.Errorf("AffectsImpactFromSpec = %v, want %v (overlap, disjoint excluded)", got, want)
	}
}

func TestAffectsImpact_ArchivedExcluded(t *testing.T) {
	tree := buildTestTree(map[string]*Spec{
		"local/s.md": {Status: StatusValidated, Affects: []string{"internal/runner/"}},
		"local/a.md": {Status: StatusArchived, Affects: []string{"internal/runner/execute.go"}},
	})
	if got := AffectsImpactFromSpec(tree, "local/s.md"); len(got) != 0 {
		t.Errorf("archived spec should be excluded, got %v", got)
	}
	if got := AffectsImpactFromDiff(tree, []string{"internal/runner/execute.go"}, "local/s.md"); len(got) != 0 {
		t.Errorf("archived spec should be excluded from diff impact, got %v", got)
	}
}

func TestFanOutStale(t *testing.T) {
	dir := t.TempDir()
	// Statuses chosen to exercise legal/illegal transitions to stale.
	files := map[string]Status{
		"validated.md": StatusValidated, // → stale (legal)
		"complete.md":  StatusComplete,  // → stale (legal)
		"vague.md":     StatusVague,     // vague → stale is illegal; skipped
		"stale.md":     StatusStale,     // already stale; same-to-same skipped
		"archived.md":  StatusArchived,  // skipped (archived)
	}
	specs := make(map[string]*Spec, len(files))
	resolve := func(p string) string { return filepath.Join(dir, filepath.Base(p)) }
	for name, st := range files {
		writeStatusSpec(t, dir, name, st)
		key := "local/" + name
		specs[key] = &Spec{Status: st}
	}
	tree := buildTestTree(specs)

	impacted := []string{
		"local/validated.md", "local/complete.md", "local/vague.md",
		"local/stale.md", "local/archived.md",
	}
	now := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	applied, err := FanOutStale(tree, impacted, resolve, now)
	if err != nil {
		t.Fatalf("FanOutStale: %v", err)
	}
	want := []string{"local/complete.md", "local/validated.md"}
	if !slices.Equal(applied, want) {
		t.Fatalf("applied = %v, want %v", applied, want)
	}
	// The two legal transitions must have actually been written to disk.
	for _, name := range []string{"validated.md", "complete.md"} {
		s, perr := ParseFile(filepath.Join(dir, name))
		if perr != nil {
			t.Fatalf("parse %s: %v", name, perr)
		}
		if s.Status != StatusStale {
			t.Errorf("%s status = %q, want stale", name, s.Status)
		}
	}
	// vague stays vague (illegal transition not applied).
	s, _ := ParseFile(filepath.Join(dir, "vague.md"))
	if s.Status != StatusVague {
		t.Errorf("vague.md should be untouched, got %q", s.Status)
	}
}

func TestCheckAffectsTooBroad(t *testing.T) {
	specs := map[string]*Spec{
		// Umbrella entry overlaps every internal/pkgN spec below.
		"local/umbrella.md": {Status: StatusValidated, Affects: []string{"internal/"}},
	}
	for i := range affectsTooBroadThreshold + 1 {
		key := fmt.Sprintf("local/p%02d.md", i)
		specs[key] = &Spec{Status: StatusValidated, Affects: []string{fmt.Sprintf("internal/pkg%02d/", i)}}
	}
	tree := buildTestTree(specs)

	results := checkAffectsTooBroad(tree)
	var flagged bool
	for _, r := range results {
		if r.Path == "local/umbrella.md" && r.Rule == "affects-too-broad" {
			flagged = true
		}
	}
	if !flagged {
		t.Errorf("umbrella spec affecting internal/ should be flagged affects-too-broad, got %v", results)
	}

	// A narrow entry that overlaps few specs is not flagged.
	narrow := buildTestTree(map[string]*Spec{
		"local/a.md": {Status: StatusValidated, Affects: []string{"internal/runner/execute.go"}},
		"local/b.md": {Status: StatusValidated, Affects: []string{"internal/runner/execute.go"}},
	})
	if got := checkAffectsTooBroad(narrow); len(got) != 0 {
		t.Errorf("narrow affects should not be flagged, got %v", got)
	}
}

// writeStatusSpec writes a minimal valid spec file at the given status.
func writeStatusSpec(t *testing.T, dir, name string, st Status) {
	t.Helper()
	content := fmt.Sprintf(`---
title: %s
status: %s
effort: small
created: 2026-01-01
updated: 2026-01-02
author: test
---

# Body

Content.
`, name, st)
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
