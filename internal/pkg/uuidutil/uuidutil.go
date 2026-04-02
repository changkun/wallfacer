// Package uuidutil provides lightweight UUID format validation without allocation.
package uuidutil

// IsValid reports whether s has the canonical UUID string format
// (xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx, 36 chars with hyphens at fixed positions).
func IsValid(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		// Positions 8, 13, 18, 23 are the four hyphen separators in a UUID:
		// xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				return false
			}
		}
	}
	return true
}
