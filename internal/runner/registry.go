package runner

import (
	"sync"

	"github.com/google/uuid"
)

// singletonKey is the fixed key used by SetSingleton/GetSingleton/DeleteSingleton.
// uuid.Nil (all-zeros) is safe because real task UUIDs are always randomly generated
// via crypto/rand and will never be all-zeros in practice.
var singletonKey = uuid.Nil

// containerRegistry is a type-safe wrapper around sync.Map for tracking
// active container names keyed by task UUID.
//
// Note: Range is never called on the ideateContainer registry (it holds at most
// one singleton entry). The Range method is provided for completeness.
type containerRegistry struct {
	m sync.Map
}

// Set stores name under id.
func (r *containerRegistry) Set(id uuid.UUID, name string) {
	r.m.Store(id, name)
}

// Get returns the container name for id and whether it was found.
func (r *containerRegistry) Get(id uuid.UUID) (string, bool) {
	v, ok := r.m.Load(id)
	if !ok {
		return "", false
	}
	return v.(string), true
}

// Delete removes the entry for id.
func (r *containerRegistry) Delete(id uuid.UUID) {
	r.m.Delete(id)
}

// Range calls fn for each entry. Iteration stops if fn returns false.
func (r *containerRegistry) Range(fn func(uuid.UUID, string) bool) {
	r.m.Range(func(k, v any) bool {
		return fn(k.(uuid.UUID), v.(string))
	})
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
