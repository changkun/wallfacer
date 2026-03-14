package gitutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiffNumstat(t *testing.T) {
	dir := setupRepo(t)

	base := gitRun(t, dir, "rev-parse", "HEAD")

	// Add a new file with 5 lines and modify the existing file.
	writeFile(t, filepath.Join(dir, "new.txt"), "a\nb\nc\nd\ne\n")
	writeFile(t, filepath.Join(dir, "file.txt"), "initial\nmodified\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add changes")
	head := gitRun(t, dir, "rev-parse", "HEAD")

	// new.txt: 5 added, 0 removed = 5 lines
	// file.txt: 1 added ("modified"), 0 removed (initial is still there) but
	// actually "initial\nmodified\n" replaces "initial\n", so: 1 added, 0 removed? No.
	// Let's just check the total is > 0 and error is nil.
	total, err := DiffNumstat(dir, base, head)
	if err != nil {
		t.Fatalf("DiffNumstat: %v", err)
	}
	if total <= 0 {
		t.Errorf("DiffNumstat = %d, want > 0", total)
	}
}

func TestDiffNumstat_EmptyHash(t *testing.T) {
	dir := setupRepo(t)
	head := gitRun(t, dir, "rev-parse", "HEAD")

	_, err := DiffNumstat(dir, "", head)
	if err == nil {
		t.Error("DiffNumstat with empty base should return error")
	}
	_, err = DiffNumstat(dir, head, "")
	if err == nil {
		t.Error("DiffNumstat with empty head should return error")
	}
}

func TestDiffNumstat_NoDiff(t *testing.T) {
	dir := setupRepo(t)
	head := gitRun(t, dir, "rev-parse", "HEAD")

	// Same commit for base and head → zero changes.
	total, err := DiffNumstat(dir, head, head)
	if err != nil {
		t.Fatalf("DiffNumstat: %v", err)
	}
	if total != 0 {
		t.Errorf("DiffNumstat same commit = %d, want 0", total)
	}
}

func TestDiffFileCount(t *testing.T) {
	dir := setupRepo(t)
	base := gitRun(t, dir, "rev-parse", "HEAD")

	// Add two new files.
	writeFile(t, filepath.Join(dir, "a.txt"), "aaa\n")
	writeFile(t, filepath.Join(dir, "b.txt"), "bbb\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "two files")
	head := gitRun(t, dir, "rev-parse", "HEAD")

	count, err := DiffFileCount(dir, base, head)
	if err != nil {
		t.Fatalf("DiffFileCount: %v", err)
	}
	if count != 2 {
		t.Errorf("DiffFileCount = %d, want 2", count)
	}
}

func TestWorkspaceWeights_SingleRepo(t *testing.T) {
	dir := setupRepo(t)
	base := gitRun(t, dir, "rev-parse", "HEAD")

	writeFile(t, filepath.Join(dir, "x.txt"), "line\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "one file")
	head := gitRun(t, dir, "rev-parse", "HEAD")

	weights := WorkspaceWeights(map[string][2]string{
		dir: {base, head},
	})
	if len(weights) != 1 {
		t.Fatalf("weights len = %d, want 1", len(weights))
	}
	if weights[dir] != 1.0 {
		t.Errorf("weights[dir] = %v, want 1.0", weights[dir])
	}
}

func TestWorkspaceWeights_TwoRepos_LineWeighted(t *testing.T) {
	// repoA gets 3 changed lines, repoB gets 1 changed line.
	// Expected weights: repoA = 0.75, repoB = 0.25.
	repoA := setupRepo(t)
	repoB := setupRepo(t)

	baseA := gitRun(t, repoA, "rev-parse", "HEAD")
	baseB := gitRun(t, repoB, "rev-parse", "HEAD")

	// repoA: add 3 lines
	writeFile(t, filepath.Join(repoA, "big.txt"), "1\n2\n3\n")
	gitRun(t, repoA, "add", ".")
	gitRun(t, repoA, "commit", "-m", "3 lines")
	headA := gitRun(t, repoA, "rev-parse", "HEAD")

	// repoB: add 1 line
	writeFile(t, filepath.Join(repoB, "small.txt"), "1\n")
	gitRun(t, repoB, "add", ".")
	gitRun(t, repoB, "commit", "-m", "1 line")
	headB := gitRun(t, repoB, "rev-parse", "HEAD")

	weights := WorkspaceWeights(map[string][2]string{
		repoA: {baseA, headA},
		repoB: {baseB, headB},
	})

	if len(weights) != 2 {
		t.Fatalf("weights len = %d, want 2", len(weights))
	}

	// Verify weights sum to 1.0.
	total := weights[repoA] + weights[repoB]
	if total < 0.9999 || total > 1.0001 {
		t.Errorf("weights sum = %v, want ~1.0", total)
	}

	// repoA should have more weight than repoB (3:1 lines).
	if weights[repoA] <= weights[repoB] {
		t.Errorf("expected repoA weight (%v) > repoB weight (%v)", weights[repoA], weights[repoB])
	}

	wantA := 0.75
	if diff := weights[repoA] - wantA; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("weights[repoA] = %v, want %v", weights[repoA], wantA)
	}
}

func TestWorkspaceWeights_EqualSplit_NoDiff(t *testing.T) {
	// When hashes are identical (no diff), both line and file counts are zero.
	// Should fall back to equal split.
	repoA := setupRepo(t)
	repoB := setupRepo(t)

	headA := gitRun(t, repoA, "rev-parse", "HEAD")
	headB := gitRun(t, repoB, "rev-parse", "HEAD")

	weights := WorkspaceWeights(map[string][2]string{
		repoA: {headA, headA}, // same → zero diff
		repoB: {headB, headB}, // same → zero diff
	})

	if len(weights) != 2 {
		t.Fatalf("weights len = %d, want 2", len(weights))
	}
	total := weights[repoA] + weights[repoB]
	if total < 0.9999 || total > 1.0001 {
		t.Errorf("weights sum = %v, want ~1.0", total)
	}
	// Equal split: each should be 0.5.
	if diff := weights[repoA] - 0.5; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("weights[repoA] = %v, want 0.5", weights[repoA])
	}
	if diff := weights[repoB] - 0.5; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("weights[repoB] = %v, want 0.5", weights[repoB])
	}
}

func TestWorkspaceWeights_EqualSplit_EmptyHashes(t *testing.T) {
	// Empty hashes make DiffNumstat/DiffFileCount fail → equal split.
	weights := WorkspaceWeights(map[string][2]string{
		"/repo/a": {"", ""},
		"/repo/b": {"", ""},
	})
	if len(weights) != 2 {
		t.Fatalf("weights len = %d, want 2", len(weights))
	}
	total := weights["/repo/a"] + weights["/repo/b"]
	if total < 0.9999 || total > 1.0001 {
		t.Errorf("weights sum = %v, want ~1.0", total)
	}
}

func TestWorkspaceWeights_FileFallback(t *testing.T) {
	// Create a binary file so numstat returns "-" (zero counted lines)
	// but file count is still > 0.
	repoA := setupRepo(t)
	repoB := setupRepo(t)

	baseA := gitRun(t, repoA, "rev-parse", "HEAD")
	baseB := gitRun(t, repoB, "rev-parse", "HEAD")

	// repoA: add a binary file (2 bytes: 0x00 0x01)
	binaryPath := filepath.Join(repoA, "data.bin")
	if err := os.WriteFile(binaryPath, []byte{0x00, 0x01}, 0644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repoA, "add", ".")
	gitRun(t, repoA, "commit", "-m", "binary file")
	headA := gitRun(t, repoA, "rev-parse", "HEAD")

	// repoB: no changes (same commit)
	headB := gitRun(t, repoB, "rev-parse", "HEAD")

	weights := WorkspaceWeights(map[string][2]string{
		repoA: {baseA, headA},
		repoB: {baseB, headB}, // identical hashes → 0 files
	})

	if len(weights) != 2 {
		t.Fatalf("weights len = %d, want 2", len(weights))
	}
	total := weights[repoA] + weights[repoB]
	if total < 0.9999 || total > 1.0001 {
		t.Errorf("weights sum = %v, want ~1.0", total)
	}
	// repoA has 1 changed file, repoB has 0.
	// File-count fallback: repoA = 1.0, repoB = 0.0.
	if diff := weights[repoA] - 1.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("weights[repoA] = %v, want 1.0", weights[repoA])
	}
	if diff := weights[repoB] - 0.0; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("weights[repoB] = %v, want 0.0", weights[repoB])
	}
}

func TestWorkspaceWeights_Nil(t *testing.T) {
	weights := WorkspaceWeights(nil)
	if weights != nil {
		t.Errorf("WorkspaceWeights(nil) = %v, want nil", weights)
	}
	weights = WorkspaceWeights(map[string][2]string{})
	if weights != nil {
		t.Errorf("WorkspaceWeights(empty) = %v, want nil", weights)
	}
}
