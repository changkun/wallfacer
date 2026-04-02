package uuidutil

import "testing"

// TestIsValid validates the UUID format checker against valid, invalid, and
// edge-case inputs merged from sandbox and logger test suites.
func TestIsValid(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Valid UUIDs.
		{"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", true},
		{"12345678-1234-1234-1234-123456789abc", true},
		{"AABBCCDD-EEFF-0011-2233-445566778899", true},
		{"550e8400-e29b-41d4-a716-446655440000", true},
		{"AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE", true},

		// Empty and short strings.
		{"", false},
		{"not-a-uuid", false},
		{"too-short", false},

		// Wrong length.
		{"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeee", false},   // 35 chars
		{"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeeee", false}, // 37 chars
		{"550e8400-e29b-41d4-a716-44665544000", false},   // 35 chars
		{"550e8400-e29b-41d4-a716-4466554400000", false}, // 37 chars

		// Wrong separator positions.
		{"aaaaaaaa_bbbb-cccc-dddd-eeeeeeeeeeee", false}, // underscore at pos 8
		{"550e8400Xe29b-41d4-a716-446655440000", false}, // X at pos 8
		{"550e8400-e29bX41d4-a716-446655440000", false}, // X at pos 13
		{"550e8400-e29b-41d4Xa716-446655440000", false}, // X at pos 18
		{"550e8400-e29b-41d4-a716X446655440000", false}, // X at pos 23

		// Non-hex characters.
		{"gaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", false},
		{"gggggggg-e29b-41d4-a716-446655440000", false},
	}
	for _, tt := range tests {
		if got := IsValid(tt.input); got != tt.want {
			t.Errorf("IsValid(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
