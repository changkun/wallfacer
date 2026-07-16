package handler

import (
	"context"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"latere.ai/x/wallfacer/internal/pkg/httpjson"
)

// artifactContentTypes whitelists the web content types Wallfacer serves from a
// workspace artifacts directory. Anything outside this set (source files,
// archives, binaries) is treated as not-found, so the directory cannot be used
// to read arbitrary repository files and no response is ever MIME-sniffed.
var artifactContentTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".htm":         "text/html; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".json":        "application/json; charset=utf-8",
	".map":         "application/json; charset=utf-8",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".jpg":         "image/jpeg",
	".jpeg":        "image/jpeg",
	".gif":         "image/gif",
	".webp":        "image/webp",
	".avif":        "image/avif",
	".ico":         "image/x-icon",
	".woff":        "font/woff",
	".woff2":       "font/woff2",
	".ttf":         "font/ttf",
	".otf":         "font/otf",
	".txt":         "text/plain; charset=utf-8",
	".xml":         "application/xml; charset=utf-8",
	".webmanifest": "application/manifest+json",
}

// artifactContentType returns the whitelisted content type for a file name and
// whether the extension is servable at all.
func artifactContentType(name string) (string, bool) {
	ct, ok := artifactContentTypes[strings.ToLower(filepath.Ext(name))]
	return ct, ok
}

// artifactsRoot resolves the artifacts directory for the active workspace.
// Artifacts are served from the first configured workspace folder only, which
// is where chat and spec agents write (their CWD is workspaces[0]); this keeps
// the /artifact/<path> URL unambiguous even when a multi-folder workspace holds
// the same file name twice. Returns "" when no workspace is active.
func (h *Handler) artifactsRoot(ctx context.Context) string {
	ws := h.visibleWorkspaces(ctx)
	if len(ws) == 0 {
		return ""
	}
	return filepath.Join(ws[0], "artifacts")
}

// ArtifactInfo describes one static artifact for the gallery listing.
type ArtifactInfo struct {
	Name     string    `json:"name"`     // base file name
	Path     string    `json:"path"`     // slash path relative to the artifacts dir
	URL      string    `json:"url"`      // ready-to-open URL: /artifact/<path>
	Size     int64     `json:"size"`     // bytes
	Modified time.Time `json:"modified"` // last modification time
}

// ListArtifacts lists the servable web files under <workspace[0]>/artifacts/,
// newest first. Non-web files and generated/dependency directories are omitted.
func (h *Handler) ListArtifacts(w http.ResponseWriter, r *http.Request) {
	out := make([]ArtifactInfo, 0, 16)
	root := h.artifactsRoot(r.Context())
	if root != "" {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // unreadable entry: skip, keep listing the rest
			}
			if d.IsDir() {
				if p != root && (strings.HasPrefix(d.Name(), ".") || skipDirs[d.Name()]) {
					return filepath.SkipDir
				}
				return nil
			}
			if _, ok := artifactContentType(d.Name()); !ok {
				return nil
			}
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			var size int64
			var mod time.Time
			if info, ierr := d.Info(); ierr == nil {
				size = info.Size()
				mod = info.ModTime()
			}
			out = append(out, ArtifactInfo{
				Name:     d.Name(),
				Path:     rel,
				URL:      (&url.URL{Path: "/artifact/" + rel}).String(),
				Size:     size,
				Modified: mod,
			})
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	httpjson.Write(w, http.StatusOK, map[string]any{"artifacts": out})
}

// ServeArtifact serves one static artifact file from <workspace[0]>/artifacts/.
// Access is confined to that directory via os.OpenRoot, so path traversal and
// symlinks that escape the root are rejected by the runtime; only whitelisted
// web content types are served, and responses are marked no-store so an
// artifact re-rendered in place is never served stale.
func (h *Handler) ServeArtifact(w http.ResponseWriter, r *http.Request) {
	rel := r.PathValue("path")
	if rel == "" {
		http.NotFound(w, r)
		return
	}
	ctype, ok := artifactContentType(rel)
	if !ok {
		http.NotFound(w, r)
		return
	}
	base := h.artifactsRoot(r.Context())
	if base == "" {
		http.NotFound(w, r)
		return
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = root.Close() }()

	f, err := root.Open(filepath.FromSlash(rel))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
