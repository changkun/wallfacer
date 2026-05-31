package main

import (
	"strings"
	"testing"
)

func TestBuildEmptyPrefix(t *testing.T) {
	got := Build("", "<!-- release-evidence -->\n\n## Release Evidence\n\n- x\n")
	want := "<!-- release-evidence -->\n\n## Release Evidence\n\n- x\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildPreservesManualNotes(t *testing.T) {
	prefix := "# Lux v1\n\n## What Changed\n\n- thing one\n"
	evidence := Marker + "\n\n## Release Evidence\n\n- ok\n"
	got := Build(prefix, evidence)
	if !strings.Contains(got, "## What Changed") {
		t.Fatalf("manual notes lost: %q", got)
	}
	if !strings.Contains(got, "- ok") {
		t.Fatalf("evidence missing: %q", got)
	}
	if strings.Count(got, Marker) != 1 {
		t.Fatalf("marker count mismatch: %q", got)
	}
}

func TestBuildIdempotentReplace(t *testing.T) {
	prefix := "# Lux v1\n\n## What Changed\n\n- thing one\n"
	first := Marker + "\n\n## Release Evidence\n\n- first run\n"
	second := Marker + "\n\n## Release Evidence\n\n- second run\n"

	once := Build(prefix, first)
	twice := Build(once, second)

	if strings.Contains(twice, "first run") {
		t.Fatalf("stale evidence persisted: %q", twice)
	}
	if !strings.Contains(twice, "second run") {
		t.Fatalf("new evidence missing: %q", twice)
	}
	if strings.Count(twice, Marker) != 1 {
		t.Fatalf("marker count mismatch: %q", twice)
	}
	if !strings.Contains(twice, "## What Changed") {
		t.Fatalf("manual notes lost on re-run: %q", twice)
	}
}

func TestBuildStripsTrailingWhitespaceFromPrefix(t *testing.T) {
	prefix := "# Lux v1\n\n## What Changed\n\n- thing\n\n\n"
	evidence := Marker + "\n\n## Release Evidence\n"
	got := Build(prefix, evidence)
	want := "# Lux v1\n\n## What Changed\n\n- thing\n\n" + Marker + "\n\n## Release Evidence\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
