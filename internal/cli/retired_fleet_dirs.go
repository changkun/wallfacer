package cli

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
)

// retiredFleetDirs names the directories under the config dir that held
// user-authored agent and fleet definitions as YAML. Wallfacer has no
// user-authored agents or fleets: nothing reads these directories, and their
// contents stay on disk because they are the user's own files.
var retiredFleetDirs = []string{"agents", "flows"}

// warnRetiredFleetDirs logs one warning for each retired directory under
// configDir that holds a file, naming the directory and saying its contents
// are no longer used. It lists directory entries only: no file is opened, and
// nothing is moved or deleted. A directory that is absent or empty logs
// nothing, so an instance that never stored a definition stays quiet. A
// directory that cannot be listed is logged and skipped; startup continues
// either way.
func warnRetiredFleetDirs(configDir string, log *slog.Logger) {
	// An empty config dir would resolve the directories under the process's
	// working directory, which wallfacer never wrote.
	if configDir == "" {
		return
	}
	for _, name := range retiredFleetDirs {
		dir := filepath.Join(configDir, name)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				log.Warn("retired agent and fleet directory: cannot inspect", "path", dir, "error", err)
			}
			continue
		}
		if !slices.ContainsFunc(entries, func(e fs.DirEntry) bool { return !e.IsDir() }) {
			continue
		}
		log.Warn("retired agent and fleet directory: its contents are no longer used, since wallfacer has no user-authored agents or fleets", "path", dir)
	}
}
