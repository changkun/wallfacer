package gitutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStashIfDirty(t *testing.T) {
	t.Run("clean tree returns false", func(t *testing.T) {
		if StashIfDirty(setupRepo(t)) {
			t.Error("StashIfDirty on clean tree = true, want false")
		}
	})

	t.Run("untracked file returns true", func(t *testing.T) {
		repo := setupRepo(t)
		writeFile(t, filepath.Join(repo, "untracked.txt"), "new\n")
		if !StashIfDirty(repo) {
			t.Error("StashIfDirty with untracked file = false, want true")
		}
	})

	t.Run("modified tracked file returns true", func(t *testing.T) {
		repo := setupRepo(t)
		writeFile(t, filepath.Join(repo, "file.txt"), "modified\n")
		if !StashIfDirty(repo) {
			t.Error("StashIfDirty with modified file = false, want true")
		}
	})
}

func TestStashPop(t *testing.T) {
	t.Run("restores stashed file", func(t *testing.T) {
		repo := setupRepo(t)
		writeFile(t, filepath.Join(repo, "stash-me.txt"), "stashed\n")
		if !StashIfDirty(repo) {
			t.Fatal("expected stash to be created")
		}
		StashPop(repo)
		if _, err := os.Stat(filepath.Join(repo, "stash-me.txt")); os.IsNotExist(err) {
			t.Error("stashed file not restored after StashPop")
		}
	})

	t.Run("no stash entry does not panic", func(t *testing.T) {
		StashPop(setupRepo(t))
	})
}
