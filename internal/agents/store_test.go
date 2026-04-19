package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUserAgents_MissingDirIsNotError(t *testing.T) {
	roles, err := LoadUserAgents(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("LoadUserAgents: %v", err)
	}
	if len(roles) != 0 {
		t.Errorf("roles = %v, want empty", roles)
	}
}

func TestLoadUserAgents_ReadsValidYAML(t *testing.T) {
	dir := t.TempDir()
	yaml := `slug: impl-codex
title: Implementation (Codex)
description: Implementation pipeline pinned to Codex.
harness: codex
capabilities: [workspace.read, workspace.write]
multiturn: true
`
	if err := os.WriteFile(filepath.Join(dir, "impl-codex.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	roles, err := LoadUserAgents(dir)
	if err != nil {
		t.Fatalf("LoadUserAgents: %v", err)
	}
	if len(roles) != 1 {
		t.Fatalf("roles = %d, want 1", len(roles))
	}
	r := roles[0]
	if r.Slug != "impl-codex" || r.Harness != "codex" || !r.Multiturn {
		t.Errorf("role fields: got %+v", r)
	}
	if len(r.Capabilities) != 2 {
		t.Errorf("capabilities: got %v", r.Capabilities)
	}
}

func TestLoadUserAgents_RejectsMalformedYAML(t *testing.T) {
	dir := t.TempDir()
	// Missing required title.
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("slug: ok-slug"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadUserAgents(dir); err == nil {
		t.Fatal("expected error for missing title")
	}
}

func TestLoadUserAgents_RejectsInvalidSlug(t *testing.T) {
	dir := t.TempDir()
	body := "slug: Bad_Slug\ntitle: Bad\n"
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadUserAgents(dir); err == nil {
		t.Fatal("expected error for invalid slug")
	}
}

func TestLoadUserAgents_SkipsNonYAMLFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("notes"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	roles, err := LoadUserAgents(dir)
	if err != nil {
		t.Fatalf("LoadUserAgents: %v", err)
	}
	if len(roles) != 0 {
		t.Errorf("roles = %v, want empty", roles)
	}
}

func TestNewMergedRegistry_IncludesBuiltinAndUser(t *testing.T) {
	dir := t.TempDir()
	body := "slug: custom-one\ntitle: Custom One\nharness: codex\n"
	if err := os.WriteFile(filepath.Join(dir, "custom-one.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	reg, err := NewMergedRegistry(dir)
	if err != nil {
		t.Fatalf("NewMergedRegistry: %v", err)
	}
	if _, ok := reg.Get("custom-one"); !ok {
		t.Error("expected custom-one in merged registry")
	}
	// Built-ins still present.
	for _, b := range BuiltinAgents {
		if _, ok := reg.Get(b.Slug); !ok {
			t.Errorf("built-in %q missing from merged registry", b.Slug)
		}
	}
}

func TestNewMergedRegistry_RejectsBuiltinShadow(t *testing.T) {
	dir := t.TempDir()
	// Shadow the "impl" built-in.
	body := "slug: impl\ntitle: Shadowed\n"
	if err := os.WriteFile(filepath.Join(dir, "impl.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := NewMergedRegistry(dir); err == nil {
		t.Fatal("expected error for built-in slug shadow")
	}
}

func TestWriteAndDeleteUserAgent_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	role := Role{Slug: "test-agent", Title: "Test Agent", Harness: "claude"}
	if err := WriteUserAgent(dir, role); err != nil {
		t.Fatalf("WriteUserAgent: %v", err)
	}
	roles, err := LoadUserAgents(dir)
	if err != nil {
		t.Fatalf("LoadUserAgents: %v", err)
	}
	if len(roles) != 1 || roles[0].Slug != "test-agent" {
		t.Fatalf("roundtrip: got %v", roles)
	}
	if err := DeleteUserAgent(dir, "test-agent"); err != nil {
		t.Fatalf("DeleteUserAgent: %v", err)
	}
	// Second delete is idempotent.
	if err := DeleteUserAgent(dir, "test-agent"); err != nil {
		t.Errorf("second delete: %v", err)
	}
	roles, _ = LoadUserAgents(dir)
	if len(roles) != 0 {
		t.Errorf("post-delete: got %v", roles)
	}
}

func TestIsValidSlug(t *testing.T) {
	cases := map[string]bool{
		"impl":               true,
		"impl-codex":         true,
		"ag01":               true,
		"a":                  false, // too short
		"-leading":           false,
		"trailing-":          false,
		"Upper":              false,
		"with_underscore":    false,
		"this-is-way-too-long-to-fit-in-forty-characters": false,
	}
	for s, want := range cases {
		if got := IsValidSlug(s); got != want {
			t.Errorf("IsValidSlug(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestIsBuiltin(t *testing.T) {
	if !IsBuiltin("impl") {
		t.Error("impl should be built-in")
	}
	if IsBuiltin("impl-codex") {
		t.Error("impl-codex should not be built-in")
	}
}
