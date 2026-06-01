// Package slugutil validates kebab-case identifiers shared across
// user-authored YAML registries (agents, flows, ...).
//
// A valid slug is 2–40 characters long, made up of lowercase ASCII
// letters, digits, and interior hyphens (no leading or trailing
// hyphen). The same shape is enforced everywhere a user picks an
// identifier for an agent role, a flow, or a system prompt name.
package slugutil

// MinLen and MaxLen bound the slug length, inclusive.
const (
	MinLen = 2
	MaxLen = 40
)

// IsValid reports whether s satisfies the slug grammar.
func IsValid(s string) bool {
	if len(s) < MinLen || len(s) > MaxLen {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
			if i == 0 || i == len(s)-1 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
