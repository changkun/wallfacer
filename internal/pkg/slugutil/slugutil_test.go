package slugutil

import "testing"

func TestIsValid(t *testing.T) {
	cases := []struct {
		in   string
		want bool
		why  string
	}{
		{"ab", true, "minimum length 2"},
		{"a", false, "below minimum length"},
		{"", false, "empty"},
		{"abc-def", true, "interior hyphen ok"},
		{"abc--def", true, "consecutive interior hyphens ok"},
		{"-abc", false, "leading hyphen"},
		{"abc-", false, "trailing hyphen"},
		{"Abc", false, "uppercase rejected"},
		{"abc_def", false, "underscore rejected"},
		{"abc def", false, "space rejected"},
		{"abc1", true, "digit ok"},
		{"1abc", true, "leading digit ok"},
		{"abc.def", false, "dot rejected"},
		{"abcdefghijklmnopqrstuvwxyz0123456789-abc", true, "exactly 40 chars"},
		{"abcdefghijklmnopqrstuvwxyz0123456789-abcd", false, "41 chars over limit"},
	}
	for _, c := range cases {
		if got := IsValid(c.in); got != c.want {
			t.Errorf("IsValid(%q) = %v, want %v (%s)", c.in, got, c.want, c.why)
		}
	}
}
