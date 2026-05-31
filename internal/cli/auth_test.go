package cli

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"golang.org/x/oauth2"
	"latere.ai/x/pkg/authkit"
)

// TestRunAuthLogout_RemovesToken verifies the logout subcommand calls
// Clear on the configured FileTokenStore so subsequent whoami reports
// "not signed in".
func TestRunAuthLogout_RemovesToken(t *testing.T) {
	dir := t.TempDir()
	// Redirect the user config dir to a tempdir so the test does not
	// touch ~/.config/latere/token.json on a real machine.
	t.Setenv("XDG_CONFIG_HOME", dir)

	storePath, err := authkit.DefaultFileTokenStorePath()
	if err != nil {
		t.Fatal(err)
	}
	store, err := authkit.NewFileTokenStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(&oauth2.Token{AccessToken: "tok"}); err != nil {
		t.Fatal(err)
	}

	if err := runAuthLogout(); err != nil {
		t.Fatalf("runAuthLogout: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "latere", "token.json")); !os.IsNotExist(err) {
		t.Fatalf("token still present after logout: %v", err)
	}
}

func TestSplitFields(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{}},
		{"a", []string{"a"}},
		{"a b c", []string{"a", "b", "c"}},
		{"a, b, c", []string{"a", "b", "c"}},
		{"  a  b ", []string{"a", "b"}},
	}
	for _, tc := range cases {
		got := splitFields(tc.in)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitFields(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestFsSet(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	var v string
	fs.StringVar(&v, "k", "default", "")

	if err := fs.Parse([]string{}); err != nil {
		t.Fatal(err)
	}
	if fsSet(fs, "k") {
		t.Error("expected unset")
	}

	fs2 := flag.NewFlagSet("y", flag.ContinueOnError)
	fs2.StringVar(&v, "k", "default", "")
	if err := fs2.Parse([]string{"-k=val"}); err != nil {
		t.Fatal(err)
	}
	if !fsSet(fs2, "k") {
		t.Error("expected set")
	}
}
