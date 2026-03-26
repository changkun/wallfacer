package runner

import (
	"io"

	"changkun.de/x/wallfacer/internal/pkg/syncmap"
	"github.com/google/uuid"
)

// singletonKey is the fixed key used by SetSingleton/GetSingleton/DeleteSingleton.
// uuid.Nil (all-zeros) is safe because real task UUIDs are always randomly generated
// via crypto/rand and will never be all-zeros in practice.
var singletonKey = uuid.Nil

// containerEntry stores a container name, an optional SandboxHandle, and an
// optional log reader. Callers that use backend.Launch() store the handle
// via SetHandle(); callers that use cmdexec directly (title, refine, commit)
// store only the name via Set().
type containerEntry struct {
	name      string
	handle    SandboxHandle // nil for name-only registrations
	logReader io.ReadCloser // nil when no log tee is configured
}

// containerRegistry tracks active containers keyed by task UUID.
//
// Note: Range is never called on the ideateContainer registry (it holds at most
// one singleton entry). The Range method is provided for completeness.
type containerRegistry struct {
	syncmap.Map[uuid.UUID, containerEntry]
}

// Set stores a container name without a handle.
func (r *containerRegistry) Set(id uuid.UUID, name string) {
	r.Store(id, containerEntry{name: name})
}

// SetHandle stores a container name with a SandboxHandle and an optional
// log reader for live log streaming.
func (r *containerRegistry) SetHandle(id uuid.UUID, handle SandboxHandle, logReader io.ReadCloser) {
	r.Store(id, containerEntry{name: handle.Name(), handle: handle, logReader: logReader})
}

// Get returns the container name for id and whether it was found.
func (r *containerRegistry) Get(id uuid.UUID) (string, bool) {
	e, ok := r.Load(id)
	if !ok {
		return "", false
	}
	return e.name, true
}

// GetHandle returns the SandboxHandle for id, or nil if not found or if the
// entry was registered without a handle.
func (r *containerRegistry) GetHandle(id uuid.UUID) SandboxHandle {
	e, ok := r.Load(id)
	if !ok {
		return nil
	}
	return e.handle
}

// GetLogReader returns the log reader for id, or nil if not found or if the
// entry was registered without a log reader.
func (r *containerRegistry) GetLogReader(id uuid.UUID) io.ReadCloser {
	e, ok := r.Load(id)
	if !ok {
		return nil
	}
	return e.logReader
}

// SetSingleton stores name under the fixed singleton key (uuid.Nil).
// Used by ideateContainer, which is always a single global container.
func (r *containerRegistry) SetSingleton(name string) {
	r.Set(singletonKey, name)
}

// SetSingletonHandle stores a handle under the fixed singleton key.
func (r *containerRegistry) SetSingletonHandle(handle SandboxHandle, logReader io.ReadCloser) {
	r.SetHandle(singletonKey, handle, logReader)
}

// GetSingletonLogReader returns the singleton log reader, or nil.
func (r *containerRegistry) GetSingletonLogReader() io.ReadCloser {
	return r.GetLogReader(singletonKey)
}

// GetSingleton returns the singleton container name and whether it was found.
func (r *containerRegistry) GetSingleton() (string, bool) {
	return r.Get(singletonKey)
}

// GetSingletonHandle returns the singleton SandboxHandle, or nil.
func (r *containerRegistry) GetSingletonHandle() SandboxHandle {
	return r.GetHandle(singletonKey)
}

// DeleteSingleton removes the singleton entry.
func (r *containerRegistry) DeleteSingleton() {
	r.Delete(singletonKey)
}
