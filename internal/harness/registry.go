package harness

import (
	"slices"
	"sync"
)

var (
	registryMu sync.RWMutex
	registry   = map[ID]Harness{}
)

// Register adds h to the package-level registry. Intended to be
// called from each harness file's init(). Panics on duplicate ID so
// build-time collisions surface immediately.
func Register(h Harness) {
	registryMu.Lock()
	defer registryMu.Unlock()
	id := h.ID()
	if _, dup := registry[id]; dup {
		panic("harness: duplicate registration: " + string(id))
	}
	registry[id] = h
}

// Lookup returns the harness for id, or false if unregistered.
func Lookup(id ID) (Harness, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	h, ok := registry[id]
	return h, ok
}

// All returns the IDs of every registered harness, sorted.
func All() []ID {
	registryMu.RLock()
	defer registryMu.RUnlock()
	ids := make([]ID, 0, len(registry))
	for id := range registry {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// Default returns the harness ID to use when the user has not chosen
// one. v1 returns Claude; a follow-up may make this configurable.
func Default() ID { return Claude }

// snapshotForTest returns a copy of the current registry and replaces it
// with an empty one. Pair with restoreForTest in a t.Cleanup so a test
// that mutates the global registry does not pollute sibling tests (the
// init-time claude/codex registrations would otherwise be lost).
func snapshotForTest() map[ID]Harness {
	registryMu.Lock()
	defer registryMu.Unlock()
	prev := registry
	registry = map[ID]Harness{}
	return prev
}

// restoreForTest puts back a registry captured by snapshotForTest.
func restoreForTest(prev map[ID]Harness) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = prev
}
