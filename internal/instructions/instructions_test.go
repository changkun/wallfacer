package instructions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Key
// ---------------------------------------------------------------------------

// TestInstructionsKeyStable verifies that the same workspace list always
// produces the same key.
func TestInstructionsKeyStable(t *testing.T) {
	ws := []string{"/home/user/projectA", "/home/user/projectB"}
	k1 := Key(ws)
	k2 := Key(ws)
	if k1 != k2 {
		t.Fatalf("key should be stable: got %q then %q", k1, k2)
	}
}

// TestInstructionsKeyOrderIndependent verifies that workspace order does not
// affect the key, so wallfacer run ~/a ~/b and wallfacer run ~/b ~/a share
// the same instructions file.
func TestInstructionsKeyOrderIndependent(t *testing.T) {
	ws1 := []string{"/home/user/alpha", "/home/user/beta"}
	ws2 := []string{"/home/user/beta", "/home/user/alpha"}
	if Key(ws1) != Key(ws2) {
		t.Fatalf("key must be order-independent: %q != %q", Key(ws1), Key(ws2))
	}
}

// TestInstructionsKeyDifferentWorkspaces verifies that distinct workspace sets
// produce distinct keys.
func TestInstructionsKeyDifferentWorkspaces(t *testing.T) {
	k1 := Key([]string{"/home/user/foo"})
	k2 := Key([]string{"/home/user/bar"})
	if k1 == k2 {
		t.Fatalf("different workspaces should produce different keys, both got %q", k1)
	}
}

// TestInstructionsKeyLength verifies the key is exactly 16 hex characters.
func TestInstructionsKeyLength(t *testing.T) {
	k := Key([]string{"/some/path"})
	if len(k) != 16 {
		t.Fatalf("expected 16-char key, got %d chars: %q", len(k), k)
	}
}

// ---------------------------------------------------------------------------
// BuildContent
// ---------------------------------------------------------------------------

// TestBuildInstructionsContentDefault verifies that when no workspace
// CLAUDE.md files exist the output contains the default template and
// workspace layout section but no per-repo instructions sections.
func TestBuildInstructionsContentDefault(t *testing.T) {
	dir := t.TempDir() // no CLAUDE.md inside
	content := BuildContent([]string{dir})
	if !strings.HasPrefix(content, defaultTemplate) {
		t.Fatal("content should start with the default template")
	}
	if !strings.Contains(content, "## Workspace Layout") {
		t.Fatal("content should include workspace layout section")
	}
	name := filepath.Base(dir)
	if !strings.Contains(content, "/workspace/"+name+"/") {
		t.Fatalf("content should list workspace %q", name)
	}
	if strings.Contains(content, "## Repo-Specific Instructions") {
		t.Fatal("content should not include Repo-Specific Instructions section when no CLAUDE.md exists")
	}
}

// TestBuildInstructionsContentWithWorkspaceCLAUDE verifies that when a workspace
// has a CLAUDE.md its path is referenced in the output (not its content).
func TestBuildInstructionsContentWithWorkspaceCLAUDE(t *testing.T) {
	dir := t.TempDir()
	repoInstructions := "# My project rules\n\nDo the thing.\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(repoInstructions), 0644); err != nil {
		t.Fatal(err)
	}

	content := BuildContent([]string{dir})

	if !strings.HasPrefix(content, defaultTemplate) {
		t.Fatal("content should start with the default template")
	}

	name := filepath.Base(dir)
	expectedRef := "- `/workspace/" + name + "/CLAUDE.md`"
	if !strings.Contains(content, expectedRef) {
		t.Fatalf("expected path reference %q in content:\n%s", expectedRef, content)
	}

	// The full file content must NOT be embedded.
	if strings.Contains(content, repoInstructions) {
		t.Fatal("repo CLAUDE.md content should not be embedded; only its path should be referenced")
	}

	if !strings.Contains(content, "## Repo-Specific Instructions") {
		t.Fatal("content should include Repo-Specific Instructions section")
	}
}

// TestBuildInstructionsContentMissingCLAUDE verifies that a workspace without
// a CLAUDE.md produces no Repo-Specific Instructions section.
func TestBuildInstructionsContentMissingCLAUDE(t *testing.T) {
	dir := t.TempDir() // no CLAUDE.md
	content := BuildContent([]string{dir})
	if !strings.HasPrefix(content, defaultTemplate) {
		t.Fatal("content should start with the default template")
	}
	if strings.Contains(content, "## Repo-Specific Instructions") {
		t.Fatal("workspace without CLAUDE.md should not produce Repo-Specific Instructions section")
	}
}

// TestBuildInstructionsContentMultipleWorkspaces verifies that CLAUDE.md paths
// from several workspaces are listed in order, while workspaces without a
// CLAUDE.md are silently omitted from the reference list.
func TestBuildInstructionsContentMultipleWorkspaces(t *testing.T) {
	dirA := t.TempDir()
	dirB := t.TempDir()
	dirC := t.TempDir()

	if err := os.WriteFile(filepath.Join(dirA, "CLAUDE.md"), []byte("instructions for A\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// dirB intentionally has no CLAUDE.md

	if err := os.WriteFile(filepath.Join(dirC, "CLAUDE.md"), []byte("instructions for C\n"), 0644); err != nil {
		t.Fatal(err)
	}

	content := BuildContent([]string{dirA, dirB, dirC})

	nameA := filepath.Base(dirA)
	nameB := filepath.Base(dirB)
	nameC := filepath.Base(dirC)

	refA := "- `/workspace/" + nameA + "/CLAUDE.md`"
	refC := "- `/workspace/" + nameC + "/CLAUDE.md`"
	refB := "- `/workspace/" + nameB + "/CLAUDE.md`"

	if !strings.Contains(content, refA) {
		t.Errorf("expected path reference for workspace A: %q", refA)
	}
	if !strings.Contains(content, refC) {
		t.Errorf("expected path reference for workspace C: %q", refC)
	}
	// dirB has no CLAUDE.md — its path should not appear.
	if strings.Contains(content, refB) {
		t.Errorf("workspace B (no CLAUDE.md) should not appear in references")
	}

	// Embedded content must not be present.
	if strings.Contains(content, "instructions for A") || strings.Contains(content, "instructions for C") {
		t.Error("repo CLAUDE.md content should not be embedded in output")
	}

	// A's reference must come before C's reference.
	posA := strings.Index(content, refA)
	posC := strings.Index(content, refC)
	if posA > posC {
		t.Error("workspace A reference should appear before workspace C reference")
	}
}

// TestBuildInstructionsContentTrailingNewline verifies that the generated
// content always ends with a newline regardless of workspace CLAUDE.md state.
func TestBuildInstructionsContentTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	// Deliberately omit trailing newline in repo CLAUDE.md; the generated
	// output should still end with a newline since we only reference the path.
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("no newline at end"), 0644); err != nil {
		t.Fatal(err)
	}

	content := BuildContent([]string{dir})

	if !strings.HasSuffix(content, "\n") {
		t.Fatal("content should end with a newline")
	}
}

// ---------------------------------------------------------------------------
// Ensure
// ---------------------------------------------------------------------------

// TestEnsureWorkspaceInstructionsCreatesFile verifies that the function
// creates a new instructions file when one does not exist yet.
func TestEnsureWorkspaceInstructionsCreatesFile(t *testing.T) {
	configDir := t.TempDir()
	ws := t.TempDir()

	path, err := Ensure(configDir, []string{ws})
	if err != nil {
		t.Fatal("Ensure:", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("instructions file should exist at %q: %v", path, err)
	}

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "Workspace Instructions") {
		t.Fatalf("instructions file should contain default template, got:\n%s", data)
	}
}

// TestEnsureWorkspaceInstructionsIdempotent verifies that calling Ensure a
// second time does NOT overwrite manually edited content.
func TestEnsureWorkspaceInstructionsIdempotent(t *testing.T) {
	configDir := t.TempDir()
	ws := t.TempDir()

	path, err := Ensure(configDir, []string{ws})
	if err != nil {
		t.Fatal(err)
	}

	customContent := "# My custom instructions\n"
	if err := os.WriteFile(path, []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Calling again should not overwrite the custom content.
	path2, err := Ensure(configDir, []string{ws})
	if err != nil {
		t.Fatal(err)
	}
	if path != path2 {
		t.Fatalf("path changed between calls: %q vs %q", path, path2)
	}

	data, _ := os.ReadFile(path)
	if string(data) != customContent {
		t.Fatalf("existing content should be preserved; got:\n%s", data)
	}
}

// TestEnsureWorkspaceInstructionsIncludesWorkspaceCLAUDE verifies that a
// newly created instructions file references the workspace's own CLAUDE.md path.
func TestEnsureWorkspaceInstructionsIncludesWorkspaceCLAUDE(t *testing.T) {
	configDir := t.TempDir()
	ws := t.TempDir()

	repoInstructions := "# Project-specific rules\n"
	if err := os.WriteFile(filepath.Join(ws, "CLAUDE.md"), []byte(repoInstructions), 0644); err != nil {
		t.Fatal(err)
	}

	path, err := Ensure(configDir, []string{ws})
	if err != nil {
		t.Fatal(err)
	}

	name := filepath.Base(ws)
	data, _ := os.ReadFile(path)
	expectedRef := "- `/workspace/" + name + "/CLAUDE.md`"
	if !strings.Contains(string(data), expectedRef) {
		t.Fatalf("instructions file should reference workspace CLAUDE.md path %q; got:\n%s", expectedRef, data)
	}
	// Content must not be embedded.
	if strings.Contains(string(data), repoInstructions) {
		t.Fatal("instructions file should not embed workspace CLAUDE.md content")
	}
}

// ---------------------------------------------------------------------------
// Reinit
// ---------------------------------------------------------------------------

// TestReinitWorkspaceInstructionsOverwrites verifies that Reinit replaces any
// previously written (or manually edited) content.
func TestReinitWorkspaceInstructionsOverwrites(t *testing.T) {
	configDir := t.TempDir()
	ws := t.TempDir()

	// First write stale content.
	path, err := Ensure(configDir, []string{ws})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("stale content"), 0644); err != nil {
		t.Fatal(err)
	}

	// Now add a CLAUDE.md to the workspace and reinit.
	repoInstructions := "# Fresh instructions\n"
	if err := os.WriteFile(filepath.Join(ws, "CLAUDE.md"), []byte(repoInstructions), 0644); err != nil {
		t.Fatal(err)
	}

	path2, err := Reinit(configDir, []string{ws})
	if err != nil {
		t.Fatal(err)
	}
	if path != path2 {
		t.Fatalf("path should be stable: %q vs %q", path, path2)
	}

	name := filepath.Base(ws)
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "stale content") {
		t.Fatal("Reinit should have overwritten stale content")
	}
	expectedRef := "- `/workspace/" + name + "/CLAUDE.md`"
	if !strings.Contains(string(data), expectedRef) {
		t.Fatalf("Reinit should reference workspace CLAUDE.md path %q; got:\n%s", expectedRef, data)
	}
	// Content must not be embedded.
	if strings.Contains(string(data), repoInstructions) {
		t.Fatal("Reinit should not embed workspace CLAUDE.md content")
	}
}
