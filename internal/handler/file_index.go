package handler

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"changkun.de/x/wallfacer/internal/logger"
)

// fileIndexTTL is the default time-to-live for a cached workspace file list.
const fileIndexTTL = 30 * time.Second

// fileCacheEntry holds the file list for one workspace plus cache metadata.
type fileCacheEntry struct {
	files     []string
	builtAt   time.Time
	rootMTime time.Time // mtime of the workspace root dir at index build time
}

// fileIndex is a per-workspace cache for file listings. Each entry is
// considered fresh as long as neither the TTL has elapsed nor the workspace
// root directory mtime has advanced. On staleness the current (possibly
// outdated) list is returned immediately while a background goroutine rebuilds
// the entry.
type fileIndex struct {
	mu         sync.RWMutex
	entries    map[string]fileCacheEntry
	refreshing map[string]bool

	ttl  time.Duration
	now  func() time.Time

	// buildFn builds the file list for a workspace. Injectable for testing.
	buildFn func(ws string) ([]string, time.Time)
}

// newFileIndex returns a fileIndex with default TTL and real-time clock.
func newFileIndex() *fileIndex {
	idx := &fileIndex{
		entries:    make(map[string]fileCacheEntry),
		refreshing: make(map[string]bool),
		ttl:        fileIndexTTL,
		now:        time.Now,
	}
	idx.buildFn = buildFiles
	return idx
}

// workspaceMtime returns the modification time of the workspace root directory.
func workspaceMtime(ws string) (time.Time, error) {
	info, err := os.Stat(ws)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// buildFiles walks ws and returns paths prefixed by the workspace basename
// (e.g. "myrepo/cmd/main.go") plus the mtime of the workspace root at walk
// start. Hidden directories and entries in skipDirs are skipped. The list is
// capped at maxFileListSize.
func buildFiles(ws string) ([]string, time.Time) {
	mtime, err := workspaceMtime(ws)
	if err != nil {
		logger.Handler.Warn("file-index: workspace mtime unavailable", "workspace", ws, "error", err)
		mtime = time.Now()
	}
	files := make([]string, 0, 256)
	base := filepath.Base(ws)
	_ = filepath.Walk(ws, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logger.Handler.Warn("file-index: walk error", "path", path, "error", err)
			if info != nil && info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			name := info.Name()
			if strings.HasPrefix(name, ".") || skipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}
		if len(files) >= maxFileListSize {
			return filepath.SkipAll
		}
		rel, relErr := filepath.Rel(ws, path)
		if relErr != nil {
			logger.Handler.Warn("file-index: rel path error", "workspace", ws, "path", path, "error", relErr)
			return nil
		}
		files = append(files, filepath.ToSlash(filepath.Join(base, rel)))
		return nil
	})
	return files, mtime
}

// Files returns the cached file list for ws. On the first call it builds the
// index synchronously. On subsequent calls it returns the cached list if it is
// still fresh; otherwise it returns the (stale) cached list immediately and
// triggers a background rebuild.
func (idx *fileIndex) Files(ws string) []string {
	// Fast path: return cached entry if present.
	idx.mu.RLock()
	entry, ok := idx.entries[ws]
	idx.mu.RUnlock()

	if ok {
		mtime, err := workspaceMtime(ws)
		fresh := err == nil &&
			!mtime.After(entry.rootMTime) &&
			idx.now().Sub(entry.builtAt) <= idx.ttl
		if fresh {
			return entry.files
		}
		// Stale: serve cached data immediately, rebuild in background.
		idx.triggerRefresh(ws)
		return entry.files
	}

	// Cold cache: synchronous build.
	files, mt := idx.buildFn(ws)
	idx.mu.Lock()
	if _, exists := idx.entries[ws]; !exists {
		idx.entries[ws] = fileCacheEntry{
			files:     files,
			builtAt:   idx.now(),
			rootMTime: mt,
		}
	} else {
		// Another goroutine populated the entry while we were building; use theirs.
		files = idx.entries[ws].files
	}
	idx.mu.Unlock()
	return files
}

// triggerRefresh starts a background goroutine that rebuilds the file index for
// ws. It is a no-op if a refresh goroutine is already running for ws.
func (idx *fileIndex) triggerRefresh(ws string) {
	idx.mu.Lock()
	if idx.refreshing[ws] {
		idx.mu.Unlock()
		return
	}
	idx.refreshing[ws] = true
	idx.mu.Unlock()

	go func() {
		files, mt := idx.buildFn(ws)
		idx.mu.Lock()
		idx.entries[ws] = fileCacheEntry{
			files:     files,
			builtAt:   idx.now(),
			rootMTime: mt,
		}
		delete(idx.refreshing, ws)
		idx.mu.Unlock()
	}()
}
