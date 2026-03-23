package runner

import (
	"changkun.de/x/wallfacer/internal/pkg/syncmap"
	"github.com/google/uuid"
)

// singletonKey is the fixed key used by SetSingleton/GetSingleton/DeleteSingleton.
// uuid.Nil (all-zeros) is safe because real task UUIDs are always randomly generated
// via crypto/rand and will never be all-zeros in practice.
var singletonKey = uuid.Nil

// containerRegistry tracks active container names keyed by task UUID.
//
// Note: Range is never called on the ideateContainer registry (it holds at most
// one singleton entry). The Range method is provided for completeness.
type containerRegistry struct {
	syncmap.Map[uuid.UUID, string]
}

// Set stores name under id.
func (r *containerRegistry) Set(id uuid.UUID, name string) {
	r.Store(id, name)
}

// Get returns the container name for id and whether it was found.
func (r *containerRegistry) Get(id uuid.UUID) (string, bool) {
	return r.Load(id)
}

// SetSingleton stores name under the fixed singleton key (uuid.Nil).
// Used by ideateContainer, which is always a single global container.
func (r *containerRegistry) SetSingleton(name string) {
	r.Set(singletonKey, name)
}

// GetSingleton returns the singleton container name and whether it was found.
func (r *containerRegistry) GetSingleton() (string, bool) {
	return r.Get(singletonKey)
}

// DeleteSingleton removes the singleton entry.
func (r *containerRegistry) DeleteSingleton() {
	r.Delete(singletonKey)
}
