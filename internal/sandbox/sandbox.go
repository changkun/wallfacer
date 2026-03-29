// Package sandbox defines the supported sandbox runtime types.
package sandbox

import (
	"slices"
	"strings"
)

// Type identifies a sandbox runtime (e.g. "claude", "codex").
type Type string

// Sandbox runtime constants.
const (
	Claude Type = "claude"
	Codex  Type = "codex"
)

// all is the canonical list of supported sandbox types, used by All().
var all = []Type{Claude, Codex}

// All returns a copy of all known sandbox types.
func All() []Type {
	return slices.Clone(all)
}

// Parse attempts to parse value into a known sandbox Type.
func Parse(value string) (Type, bool) {
	switch Type(strings.ToLower(strings.TrimSpace(value))) {
	case Claude:
		return Claude, true
	case Codex:
		return Codex, true
	default:
		return "", false
	}
}

// Normalize returns the canonical lowercase Type, even for unknown values.
// Unknown values are lowercased rather than rejected so they can be stored
// as-is, allowing forward compatibility when new sandbox types are added.
func Normalize(value string) Type {
	if parsed, ok := Parse(value); ok {
		return parsed
	}
	return Type(strings.ToLower(strings.TrimSpace(value)))
}

// Default returns the parsed Type or Claude if the value is unrecognised.
func Default(value string) Type {
	if parsed, ok := Parse(value); ok {
		return parsed
	}
	return Claude
}

// IsValid reports whether t is a known sandbox type.
func (t Type) IsValid() bool {
	_, ok := Parse(string(t))
	return ok
}

// OrDefault returns t if valid, otherwise Claude.
func (t Type) OrDefault() Type {
	if t.IsValid() {
		return t
	}
	return Claude
}
