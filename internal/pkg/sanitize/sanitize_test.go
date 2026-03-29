package sanitize

import "testing"

func TestBasename(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"/home/user/my-repo", "my-repo"},
		{"/home/user/My Project", "My_Project"},
		{"/home/user/我的项目", "我的项目"},
		{"/path/to/café-code", "café-code"},
		{"/path/to/repo:special", "repo_special"},
		{"/path/to/dir with $vars", "dir_with__vars"},
		{"/path/with/trailing/", "trailing"},
		{"", "workspace"},
		{"/", "workspace"},
		{".", "workspace"},
		{"/path/to/a`b\"c'd", "a_b_c_d"},
		{"/path/to/🚀rocket", "_rocket"},
	}
	for _, tc := range cases {
		got := Basename(tc.input)
		if got != tc.want {
			t.Errorf("Basename(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestBase(t *testing.T) {
	cases := []struct {
		input, want string
	}{
		{"my-repo", "my-repo"},
		{"My Project", "My_Project"},
		{"我的项目", "我的项目"},
		{"café-code", "café-code"},
		{"repo:special", "repo_special"},
		{"", "workspace"},
		{"/", "workspace"},
		{".", "workspace"},
		{`\`, "workspace"},
		{"a`b\"c'd", "a_b_c_d"},
	}
	for _, tc := range cases {
		got := Base(tc.input)
		if got != tc.want {
			t.Errorf("Base(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		n     int
		want  string
	}{
		{"short string unchanged", "hello", 10, "hello"},
		{"exact length unchanged", "hello", 5, "hello"},
		{"truncated adds ellipsis", "hello world", 5, "hello…"},
		{"empty string", "", 5, ""},
		{"max zero", "hello", 0, "…"},
		{"single char truncation", "abc", 1, "a…"},
		{"multi-byte rune handling", "αβγδε", 3, "αβγ…"},
		{"exact rune count no ellipsis", "αβγ", 3, "αβγ"},
		{"toolong", "toolong", 4, "tool…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Truncate(tc.input, tc.n)
			if got != tc.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tc.input, tc.n, got, tc.want)
			}
		})
	}
}

func TestSlug(t *testing.T) {
	cases := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"simple words", "Add dark mode", 30, "add-dark-mode"},
		{"special chars", "Fix bug: in #42!", 20, "fix-bug-in-42"},
		{"leading spaces", "  hello world", 20, "hello-world"},
		{"consecutive spaces", "a  b  c", 20, "a-b-c"},
		{"empty string", "", 20, "task"},
		{"all special", "!@#$%", 20, "task"},
		{"truncate", "abcdefghijklmnopqrstuvwxyz", 10, "abcdefghij"},
		{"truncate at dash boundary", "add dark mode toggle feature", 12, "add-dark-mod"},
		{"numbers preserved", "fix issue 123", 20, "fix-issue-123"},
		{"Hello World", "Hello World", 64, "hello-world"},
		{"special chars only fallback", "!!! @@@", 64, "task"},
		{"leading trailing special", "--hello--", 64, "hello"},
		{"collapses dashes", "hello   world", 64, "hello-world"},
		{"mixed input", "Fix bug in v1.2.3!", 64, "fix-bug-in-v1-2-3"},
		{"numbers", "task 42", 64, "task-42"},
		{"leading whitespace", "   abc", 64, "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Slug(tc.input, tc.maxLen)
			if got != tc.want {
				t.Errorf("Slug(%q, %d) = %q, want %q", tc.input, tc.maxLen, got, tc.want)
			}
		})
	}
}

func TestSlugMaxLen(t *testing.T) {
	input := "this is a long prompt that should be truncated"
	got := Slug(input, 10)
	if len(got) > 10 {
		t.Errorf("Slug result length %d exceeds maxLen 10: %q", len(got), got)
	}
}
