package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// resolveContainerByPrefix
// ---------------------------------------------------------------------------

func TestResolveContainerByPrefixExactMatch(t *testing.T) {
	psOutput := "wallfacer-add-dark-mode-249e9c9c\n"
	got, err := resolveContainerByPrefix(psOutput, "249e9c9c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "wallfacer-add-dark-mode-249e9c9c" {
		t.Fatalf("expected container name %q, got %q", "wallfacer-add-dark-mode-249e9c9c", got)
	}
}

func TestResolveContainerByPrefixSubstringMatch(t *testing.T) {
	// The prefix appears in the middle of the container name (slug portion).
	psOutput := "wallfacer-fix-foo-bar-abcd1234\nwallfacer-other-task-99887766\n"
	got, err := resolveContainerByPrefix(psOutput, "abcd1234")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "wallfacer-fix-foo-bar-abcd1234" {
		t.Fatalf("expected %q, got %q", "wallfacer-fix-foo-bar-abcd1234", got)
	}
}

func TestResolveContainerByPrefixNoMatch(t *testing.T) {
	psOutput := "wallfacer-add-dark-mode-249e9c9c\nwallfacer-fix-login-abcdef12\n"
	_, err := resolveContainerByPrefix(psOutput, "deadbeef")
	if err == nil {
		t.Fatal("expected error for no-match case, got nil")
	}
	if !strings.Contains(err.Error(), "no running container") {
		t.Fatalf("expected 'no running container' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "deadbeef") {
		t.Fatalf("expected prefix %q in error message, got: %v", "deadbeef", err)
	}
}

func TestResolveContainerByPrefixAmbiguous(t *testing.T) {
	// Two containers whose names both contain the prefix.
	psOutput := "wallfacer-task-a-249e9c9c\nwallfacer-task-b-249e9c9c\n"
	_, err := resolveContainerByPrefix(psOutput, "249e9c9c")
	if err == nil {
		t.Fatal("expected error for ambiguous match, got nil")
	}
	if !strings.Contains(err.Error(), "multiple containers") {
		t.Fatalf("expected 'multiple containers' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "wallfacer-task-a-249e9c9c") {
		t.Fatalf("expected first candidate listed in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "wallfacer-task-b-249e9c9c") {
		t.Fatalf("expected second candidate listed in error, got: %v", err)
	}
}

func TestResolveContainerByPrefixEmptyOutput(t *testing.T) {
	_, err := resolveContainerByPrefix("", "249e9c9c")
	if err == nil {
		t.Fatal("expected error for empty ps output, got nil")
	}
	if !strings.Contains(err.Error(), "no running container") {
		t.Fatalf("expected 'no running container' in error, got: %v", err)
	}
}

func TestResolveContainerByPrefixBlankLines(t *testing.T) {
	// Blank lines in ps output must be ignored.
	psOutput := "\n\nwallfacer-fix-auth-aabbccdd\n\n"
	got, err := resolveContainerByPrefix(psOutput, "aabbccdd")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "wallfacer-fix-auth-aabbccdd" {
		t.Fatalf("expected %q, got %q", "wallfacer-fix-auth-aabbccdd", got)
	}
}

func TestResolveContainerByPrefixMultipleContainersOneMatch(t *testing.T) {
	// Several containers are running but only one matches the prefix.
	psOutput := strings.Join([]string{
		"wallfacer-add-feature-11223344",
		"wallfacer-fix-bug-55667788",
		"wallfacer-refactor-db-99aabbcc",
		"unrelated-container-xyz",
	}, "\n")
	got, err := resolveContainerByPrefix(psOutput, "55667788")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "wallfacer-fix-bug-55667788" {
		t.Fatalf("expected %q, got %q", "wallfacer-fix-bug-55667788", got)
	}
}
