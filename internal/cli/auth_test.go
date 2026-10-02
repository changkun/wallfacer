package cli

import (
	"errors"
	"flag"
	"os"
	"reflect"
	"testing"

	"golang.org/x/oauth2"
	"latere.ai/x/pkg/authkit/cli"
)

// TestRunAuthLogout_RemovesToken verifies the logout subcommand removes the
// token file at the default store path, so a subsequent whoami reports "not
// signed in". TestMain isolates the per-user config directory, so the default
// path lies in a temporary home rather than at the user's real sign-in.
func TestRunAuthLogout_RemovesToken(t *testing.T) {
	storePath, err := cli.DefaultFileTokenStorePath()
	if err != nil {
		t.Fatal(err)
	}
	store, err := cli.NewFileTokenStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(&oauth2.Token{AccessToken: "tok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(storePath); err != nil {
		t.Fatalf("token not written to %s: %v", storePath, err)
	}

	if err := runAuthLogout(); err != nil {
		t.Fatalf("runAuthLogout: %v", err)
	}

	if _, err := os.Stat(storePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("token still present at %s after logout: %v", storePath, err)
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
		{"a\tb\nc", []string{"a", "b", "c"}}, // tab and newline delimiters
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
