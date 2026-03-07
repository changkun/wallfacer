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

// TestBuildInstructionsContentWithWorkspaceCLAUDE verifies that when a single
// workspace has a CLAUDE.md its path is referenced (not its content embedded),
// so the agent can read it directly from the workspace.
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

	// Content must NOT be embedded; only a path reference should appear.
	if strings.Contains(content, repoInstructions) {
		t.Fatalf("single-workspace repo CLAUDE.md content should not be embedded; got:\n%s", content)
	}

	name := filepath.Base(dir)
	pathRef := "- `/workspace/" + name + "/CLAUDE.md`"
	if !strings.Contains(content, pathRef) {
		t.Fatalf("single-workspace should contain path reference %q; got:\n%s", pathRef, content)
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

// TestBuildInstructionsContentSingleWorkspaceRef verifies that in single-repo
// mode a path reference appears and the content is not embedded.
func TestBuildInstructionsContentSingleWorkspaceRef(t *testing.T) {
	dir := t.TempDir()
	repoInstructions := "# Rules\nDo the thing.\n"
	if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(repoInstructions), 0644); err != nil {
		t.Fatal(err)
	}

	content := BuildContent([]string{dir})

	name := filepath.Base(dir)
	// A path reference must appear.
	pathRef := "- `/workspace/" + name + "/CLAUDE.md`"
	if !strings.Contains(content, pathRef) {
		t.Fatalf("single-workspace output should contain path reference %q", pathRef)
	}
	// Content must NOT be embedded.
	if strings.Contains(content, repoInstructions) {
		t.Fatal("single-workspace content should not be embedded")
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
	// Repo CLAUDE.md exists so a path reference section is emitted.
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
// newly created instructions file references the single workspace's CLAUDE.md path.
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

	data, _ := os.ReadFile(path)
	name := filepath.Base(ws)
	pathRef := "- `/workspace/" + name + "/CLAUDE.md`"
	// A path reference must appear; content must not be embedded.
	if !strings.Contains(string(data), pathRef) {
		t.Fatalf("instructions file should reference single-workspace CLAUDE.md path; got:\n%s", data)
	}
	if strings.Contains(string(data), repoInstructions) {
		t.Fatalf("instructions file should not embed single-workspace CLAUDE.md content; got:\n%s", data)
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

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "stale content") {
		t.Fatal("Reinit should have overwritten stale content")
	}
	// For a single workspace a path reference must appear; content must not be embedded.
	name := filepath.Base(ws)
	pathRef := "- `/workspace/" + name + "/CLAUDE.md`"
	if !strings.Contains(string(data), pathRef) {
		t.Fatalf("Reinit should reference single-workspace CLAUDE.md path; got:\n%s", data)
	}
	if strings.Contains(string(data), repoInstructions) {
		t.Fatalf("Reinit should not embed single-workspace CLAUDE.md content; got:\n%s", data)
	}
}
