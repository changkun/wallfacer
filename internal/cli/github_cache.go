package cli

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// githubTokenCacheDir names the directory under the config dir that held the
// GitHub integration's token cache: one file per principal, each a short-lived
// GitHub access token brokered through the signed-in Latere account. Wallfacer
// holds no GitHub credential and no code reads these files.
const githubTokenCacheDir = "github"

// removeGitHubTokenCache deletes <configDir>/github/ when it exists, because a
// credential that nothing reads should not stay on disk. It logs one line when
// it removes the directory and nothing when there is no directory, so only the
// first start after an upgrade reports it. A failure is logged as a warning and
// startup continues: the files are inert, and refusing to start over them would
// cost more than leaving them for the next start to retry.
func removeGitHubTokenCache(configDir string, log *slog.Logger) {
	// An empty config dir would resolve the cache to ./github under the
	// process's working directory, which wallfacer never wrote.
	if configDir == "" {
		return
	}
	dir := filepath.Join(configDir, githubTokenCacheDir)
	if _, err := os.Lstat(dir); err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Warn("github token cache: cannot inspect", "path", dir, "error", err)
		}
		return
	}
	if err := os.RemoveAll(dir); err != nil {
		log.Warn("github token cache: remove failed", "path", dir, "error", err)
		return
	}
	log.Info("github token cache: removed", "path", dir)
}
