package harness

import "strings"

// NormalizeID returns the canonical lowercase ID, even for unknown values.
// Unknown values are lowercased rather than rejected so they can be stored
// as-is, keeping forward compatibility when new harnesses are added.
func NormalizeID(value string) ID {
	return ID(strings.ToLower(strings.TrimSpace(value)))
}

// ParseID parses value into a registered harness ID. Returns ("", false)
// when no harness with that ID is registered, matching conventional Parse
// semantics (zero value on failure).
func ParseID(value string) (ID, bool) {
	id := NormalizeID(value)
	if _, ok := Lookup(id); ok {
		return id, true
	}
	return "", false
}

// DefaultFrom returns the parsed ID, or Default() if the value names no
// registered harness. Mirrors the old sandbox.Default helper.
func DefaultFrom(value string) ID {
	if id, ok := ParseID(value); ok {
		return id
	}
	return Default()
}

// IsValid reports whether id names a registered harness.
func (id ID) IsValid() bool {
	_, ok := Lookup(id)
	return ok
}

// OrDefault returns id if it names a registered harness, otherwise Default().
func (id ID) OrDefault() ID {
	if id.IsValid() {
		return id
	}
	return Default()
}
