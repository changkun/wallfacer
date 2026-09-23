package runner

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"latere.ai/x/pkg/cmdexec"
	"latere.ai/x/pkg/dircp"
	"latere.ai/x/pkg/gitutil"

	"latere.ai/x/wallfacer/internal/logger"
)

// setupNonGitSnapshot copies ws into snapshotPath and initializes a local git
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

// extractSnapshotToWorkspace copies the snapshot at snapshotPath back into the
// original workspace at targetPath.
//
// Contract, identical on both branches: every entry named ".git", at any depth
// and whether it is a directory (a repository, a vendored checkout) or a file
// (a submodule gitlink), is excluded from the copy, and the workspace's own
// .git is never modified or removed. The snapshot's tracking repository
// therefore never reaches the workspace, and a workspace that is itself a git
// repository keeps its history.
//
// The branches differ only in deletions: rsync propagates files deleted inside
// the snapshot to the workspace (excluded .git paths are protected from that),
// while the Go fallback copies new and modified files only.
func extractSnapshotToWorkspace(snapshotPath, targetPath string) error {
	// rsync handles new, modified, AND deleted files correctly via --delete.
	// --checksum is needed because files may have the same size and mtime
	// but different content (e.g. macOS openrsync skips them otherwise).
	// The trailing "/" on both paths is critical: it means "copy contents of
	// snapshotPath into targetPath" rather than creating a subdirectory.
	// --exclude=.git carries no slash, so rsync matches it against the final
	// path component at any depth; copySnapshotWithoutGit matches that.
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
	logger.Runner.Warn("rsync not found; falling back to copy (deletions will not propagate to workspace)",
		"snapshot", snapshotPath, "target", targetPath)
	if err := copySnapshotWithoutGit(snapshotPath, targetPath); err != nil {
		return fmt.Errorf("copy snapshot to workspace: %w", err)
	}
	return nil
}

// copySnapshotWithoutGit copies the tree at snapshot into target, skipping
// every entry named ".git" at any depth, of any type. It creates target and
// missing parents, overwrites files in place, and deletes nothing that the
// snapshot does not replace, so anything the workspace holds outside the
// snapshot (its own .git above all) is left as it was.
func copySnapshotWithoutGit(snapshot, target string) error {
	return filepath.WalkDir(snapshot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(snapshot, path)
		if err != nil {
			return err
		}
		if rel == "." {
			if !d.IsDir() {
				return fmt.Errorf("snapshot %q is not a directory", snapshot)
			}
			return os.MkdirAll(target, 0755)
		}
		// rsync --exclude=.git matches the basename at any depth, directory
		// or gitlink file alike; skip the subtree the same way.
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dest := filepath.Join(target, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			// The snapshot is a copy of the workspace, so the link usually
			// exists on both sides; os.Symlink refuses an existing name.
			if _, err := os.Lstat(dest); err == nil {
				if err := os.Remove(dest); err != nil {
					return err
				}
			}
			return os.Symlink(link, dest)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return dircp.CopyFile(path, dest, info.Mode())
	})
}
