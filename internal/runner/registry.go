package runner

import (
	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/executor"
	"latere.ai/x/wallfacer/internal/pkg/syncmap"
)

// containerEntry stores a container name and an optional executor.Handle.
// Callers that use backend.Launch() store the handle via SetHandle();
// callers that use cmdexec directly (title, refine, commit) store only the
// name via Set().
type containerEntry struct {
	name   string
	handle executor.Handle // nil for name-only registrations
}

// containerRegistry tracks active containers keyed by task UUID.
type containerRegistry struct {
	syncmap.Map[uuid.UUID, containerEntry]
}

// Set stores a container name without a handle.
func (r *containerRegistry) Set(id uuid.UUID, name string) {
	r.Store(id, containerEntry{name: name})
}

// SetHandle stores a container name together with its executor.Handle.
func (r *containerRegistry) SetHandle(id uuid.UUID, handle executor.Handle) {
	r.Store(id, containerEntry{name: handle.Name(), handle: handle})
}

// Get returns the container name for id and whether it was found.
func (r *containerRegistry) Get(id uuid.UUID) (string, bool) {
	e, ok := r.Load(id)
	if !ok {
		return "", false
	}
	return e.name, true
}

// GetHandle returns the executor.Handle for id, or nil if not found or if the
// entry was registered without a handle.
func (r *containerRegistry) GetHandle(id uuid.UUID) executor.Handle {
	e, ok := r.Load(id)
	if !ok {
		return nil
	}
	return e.handle
}
