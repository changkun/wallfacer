package runner

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"changkun.de/x/wallfacer/internal/gitutil"
	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
	"changkun.de/x/wallfacer/internal/pkg/dircp"
)

// setupNonGitSnapshot copies ws into snapshotPath and initialises a local git
// repo there for change tracking. This lets the standard commit pipeline work
// on non-git workspaces: Phase 1 commits changes in the snapshot, Phase 2
// copies the snapshot back to ws (instead of rebasing into a remote branch).
func setupNonGitSnapshot(ws, snapshotPath string) error {
	if err := os.MkdirAll(snapshotPath, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	if err := dircp.Copy(ws, snapshotPath); err != nil {
		if rmErr := os.RemoveAll(snapshotPath); rmErr != nil {
			logger.Runner.Warn("snapshot cleanup failed after copy error", "path", snapshotPath, "error", rmErr)
		}
		return fmt.Errorf("cp workspace to snapshot: %w", err)
	}
	if err := gitutil.InitLocalRepo(snapshotPath, "wallfacer@local", "Wallfacer", "wallfacer: initial snapshot"); err != nil {
		if rmErr := os.RemoveAll(snapshotPath); rmErr != nil {
			logger.Runner.Warn("snapshot cleanup failed after git init error", "path", snapshotPath, "error", rmErr)
		}
		return fmt.Errorf("init snapshot repo: %w", err)
	}
	return nil
}

// extractSnapshotToWorkspace copies all changes from snapshotPath back to
// the original workspace at targetPath, excluding the .git directory that was
// added for change tracking. Uses rsync when available (handles deletions);
// falls back to a Go-native copy which covers new/modified files only.
func extractSnapshotToWorkspace(snapshotPath, targetPath string) error {
	// rsync handles new, modified, AND deleted files correctly via --delete.
	// --checksum is needed because files may have the same size and mtime
	// but different content (e.g. macOS openrsync skips them otherwise).
	// The trailing "/" on both paths is critical: it means "copy contents of
	// snapshotPath into targetPath" rather than creating a subdirectory.
	if _, err := exec.LookPath("rsync"); err == nil {
		out, err := cmdexec.New(
			"rsync", "-a", "--checksum", "--delete", "--exclude=.git",
			snapshotPath+"/", targetPath+"/",
		).Combined()
		if err != nil {
			return fmt.Errorf("rsync snapshot to workspace: %w\n%s", err, out)
		}
		return nil
	}
	// Fallback: copy covers new/modified files; files deleted inside the sandbox
	// are not removed from the original workspace.
	logger.Runner.Warn("rsync not found; falling back to copy (deletions will not propagate to workspace)",
		"snapshot", snapshotPath, "target", targetPath)
	if err := dircp.Copy(snapshotPath, targetPath); err != nil {
		return fmt.Errorf("copy snapshot to workspace: %w", err)
	}
	// Remove the .git directory that the copy may have brought over from the snapshot.
	if err := os.RemoveAll(filepath.Join(targetPath, ".git")); err != nil {
		logger.Runner.Warn("failed to remove .git from extracted snapshot", "path", targetPath, "error", err)
	}
	return nil
}
