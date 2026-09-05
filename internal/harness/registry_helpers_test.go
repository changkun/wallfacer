package harness

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
