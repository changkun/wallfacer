package runner

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"changkun.de/x/wallfacer/internal/logger"
)

// setupNonGitSnapshot copies ws into snapshotPath and initialises a local git
// repo there for change tracking. This lets the standard commit pipeline work
// on non-git workspaces: Phase 1 commits changes in the snapshot, Phase 2
// copies the snapshot back to ws (instead of rebasing into a remote branch).
func setupNonGitSnapshot(ws, snapshotPath string) error {
	if err := os.MkdirAll(snapshotPath, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	if err := copyDirContents(ws, snapshotPath); err != nil {
		if rmErr := os.RemoveAll(snapshotPath); rmErr != nil {
			logger.Runner.Warn("snapshot cleanup failed after copy error", "path", snapshotPath, "error", rmErr)
		}
		return fmt.Errorf("cp workspace to snapshot: %w", err)
	}
	// Initialise a git repo so Phase 1 (hostStageAndCommit) can commit changes.
	if out, err := exec.Command("git", "-C", snapshotPath, "init").CombinedOutput(); err != nil {
		if rmErr := os.RemoveAll(snapshotPath); rmErr != nil {
			logger.Runner.Warn("snapshot cleanup failed after git init error", "path", snapshotPath, "error", rmErr)
		}
		return fmt.Errorf("git init snapshot: %w\n%s", err, out)
	}
	if err := exec.Command("git", "-C", snapshotPath, "config", "user.email", "wallfacer@local").Run(); err != nil {
		logger.Runner.Warn("snapshot git config user.email", "path", snapshotPath, "error", err)
	}
	if err := exec.Command("git", "-C", snapshotPath, "config", "user.name", "Wallfacer").Run(); err != nil {
		logger.Runner.Warn("snapshot git config user.name", "path", snapshotPath, "error", err)
	}
	if err := exec.Command("git", "-C", snapshotPath, "add", "-A").Run(); err != nil {
		logger.Runner.Warn("snapshot git add", "path", snapshotPath, "error", err)
	}
	// --allow-empty handles the edge case of an empty workspace.
	if err := exec.Command("git", "-C", snapshotPath, "commit", "--allow-empty", "-m", "wallfacer: initial snapshot").Run(); err != nil {
		logger.Runner.Warn("snapshot git commit", "path", snapshotPath, "error", err)
	}
	return nil
}

// extractSnapshotToWorkspace copies all changes from snapshotPath back to
// the original workspace at targetPath, excluding the .git directory that was
// added for change tracking. Uses rsync when available (handles deletions);
// falls back to a Go-native copy which covers new/modified files only.
func extractSnapshotToWorkspace(snapshotPath, targetPath string) error {
	// rsync handles new, modified, AND deleted files correctly.
	// --checksum is needed because files may have the same size and mtime
	// but different content (e.g. macOS openrsync skips them otherwise).
	if _, err := exec.LookPath("rsync"); err == nil {
		out, err := exec.Command(
			"rsync", "-a", "--checksum", "--delete", "--exclude=.git",
			snapshotPath+"/", targetPath+"/",
		).CombinedOutput()
		if err != nil {
			return fmt.Errorf("rsync snapshot to workspace: %w\n%s", err, out)
		}
		return nil
	}
	// Fallback: copy covers new/modified files; files deleted inside the sandbox
	// are not removed from the original workspace.
	logger.Runner.Warn("rsync not found; falling back to copy (deletions will not propagate to workspace)",
		"snapshot", snapshotPath, "target", targetPath)
	if err := copyDirContents(snapshotPath, targetPath); err != nil {
		return fmt.Errorf("copy snapshot to workspace: %w", err)
	}
	// Remove the .git directory that the copy may have brought over from the snapshot.
	if err := os.RemoveAll(filepath.Join(targetPath, ".git")); err != nil {
		logger.Runner.Warn("failed to remove .git from extracted snapshot", "path", targetPath, "error", err)
	}
	return nil
}

// copyDirContents copies all files and directories from src into dst,
// preserving directory structure. On Unix, it tries `cp -a src/. dst`
// first for speed and falls back to a pure-Go walk on failure or on Windows.
func copyDirContents(src, dst string) error {
	if runtime.GOOS != "windows" {
		out, err := exec.Command("cp", "-a", src+"/.", dst).CombinedOutput()
		if err == nil {
			return nil
		}
		logger.Runner.Warn("cp -a failed, falling back to Go copy",
			"src", src, "dst", dst, "error", fmt.Sprintf("%v: %s", err, out))
	}
	return copyDirContentsGo(src, dst)
}

// copyDirContentsGo is a pure-Go recursive directory copy.
func copyDirContentsGo(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

// copyFile copies a single file preserving permissions.
func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}
