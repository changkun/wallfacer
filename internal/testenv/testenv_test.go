package testenv

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"latere.ai/x/pkg/authkit/cli"
)

// restoreEnvAfter records every variable runIsolated may change so the test
// leaves the process environment as it found it.
func restoreEnvAfter(t *testing.T) {
	t.Helper()
	for _, k := range append([]string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "AppData", "LocalAppData", "PATH"}, goEnvVars...) {
		t.Setenv(k, os.Getenv(k))
	}
}

func TestRunIsolatedPointsPerUserLocationsIntoATempDir(t *testing.T) {
	restoreEnvAfter(t)
	// Preset Go variables leave go env unrun, so diagnostics stay empty on a
	// PATH without the go command.
	for _, k := range goEnvVars {
		t.Setenv(k, "preset")
	}
	startHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve the starting home: %v", err)
	}
	var home, tokenPath string
	var stderr bytes.Buffer
	code := runIsolated(func() int {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			t.Errorf("resolve the isolated home: %v", err)
			return 0
		}
		if tokenPath, err = cli.DefaultFileTokenStorePath(); err != nil {
			t.Errorf("resolve the token path: %v", err)
			return 0
		}
		for _, resolve := range []func() (string, error){os.UserConfigDir, os.UserCacheDir} {
			dir, err := resolve()
			if err != nil || !Within(dir, home) {
				t.Errorf("per-user directory %q (err %v) is not inside the isolated home %s", dir, err, home)
			}
		}
		return 3
	}, &stderr)

	if code != 3 {
		t.Errorf("exit code = %d, want the run function's 3", code)
	}
	if home == startHome {
		t.Errorf("home stayed at %s inside the run", home)
	}
	if !Within(tokenPath, home) || filepath.Base(tokenPath) != "token.json" {
		t.Errorf("token path %s is not a token.json inside the isolated home %s", tokenPath, home)
	}
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("isolated home %s still exists after the run: %v", home, err)
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected diagnostics: %s", stderr.String())
	}
}

func TestRunIsolatedPinsGoCachesOutsideTheIsolatedHome(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go command not on PATH: %v", err)
	}
	restoreEnvAfter(t)
	for _, k := range goEnvVars {
		t.Setenv(k, "")
	}
	got := map[string]string{}
	var home string
	var stderr bytes.Buffer
	runIsolated(func() int {
		var err error
		if home, err = os.UserHomeDir(); err != nil {
			t.Errorf("resolve the isolated home: %v", err)
		}
		for _, k := range goEnvVars {
			got[k] = os.Getenv(k)
		}
		return 0
	}, &stderr)
	if stderr.Len() != 0 {
		t.Fatalf("unexpected diagnostics: %s", stderr.String())
	}
	// TestMain already runs this process under RunIsolated, so the home go env
	// resolves against here is the outer isolated one; what the assertion pins
	// is that the values were resolved before the inner isolation moved HOME.
	for _, k := range []string{"GOCACHE", "GOMODCACHE", "GOPATH"} {
		if got[k] == "" || Within(got[k], home) {
			t.Errorf("%s = %q, want a location resolved before isolation, outside the isolated home %s", k, got[k], home)
		}
	}
}

func TestWithin(t *testing.T) {
	sep := string(filepath.Separator)
	cases := []struct {
		path, dir string
		want      bool
	}{
		{sep + "a", sep + "a", true},
		{sep + filepath.Join("a", "b"), sep + "a", true},
		{sep + "ab", sep + "a", false},
		{sep + "b", sep + "a", false},
		{sep + filepath.Join("a", "..b"), sep + "a", true},
	}
	for _, c := range cases {
		if got := Within(c.path, c.dir); got != c.want {
			t.Errorf("Within(%q, %q) = %v, want %v", c.path, c.dir, got, c.want)
		}
	}
}
