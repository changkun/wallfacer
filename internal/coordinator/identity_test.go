package coordinator

import "testing"

func TestNormalizeRemoteURL(t *testing.T) {
	// The three transports for one repo must converge to one key (the spec's
	// connection-and-presence test plan).
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"scp", "git@github.com:latere-ai/wallfacer.git", "github.com/latere-ai/wallfacer"},
		{"https", "https://github.com/latere-ai/wallfacer", "github.com/latere-ai/wallfacer"},
		{"https-dotgit", "https://github.com/latere-ai/wallfacer.git", "github.com/latere-ai/wallfacer"},
		{"ssh-url", "ssh://git@github.com/latere-ai/wallfacer.git", "github.com/latere-ai/wallfacer"},
		{"https-creds", "https://user:token@github.com/latere-ai/wallfacer.git", "github.com/latere-ai/wallfacer"},
		{"https-port", "https://github.com:443/latere-ai/wallfacer.git", "github.com/latere-ai/wallfacer"},
		{"host-uppercase", "https://GitHub.com/latere-ai/wallfacer", "github.com/latere-ai/wallfacer"},
		{"trailing-slash", "https://github.com/latere-ai/wallfacer/", "github.com/latere-ai/wallfacer"},
		{"whitespace", "  git@github.com:latere-ai/wallfacer.git\n", "github.com/latere-ai/wallfacer"},
		{"self-hosted-port", "ssh://git@git.example.com:2222/team/repo.git", "git.example.com/team/repo"},
		{"empty", "", ""},
		{"bare-path", "/local/only/path", ""},
		{"no-path", "https://github.com", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NormalizeRemoteURL(c.in); got != c.want {
				t.Fatalf("NormalizeRemoteURL(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestNormalizeRemoteURL_Converges(t *testing.T) {
	// All recognized forms of the same repo are the same join key.
	forms := []string{
		"git@github.com:latere-ai/wallfacer.git",
		"https://github.com/latere-ai/wallfacer",
		"https://github.com/latere-ai/wallfacer.git",
		"ssh://git@github.com/latere-ai/wallfacer.git",
	}
	want := NormalizeRemoteURL(forms[0])
	if want == "" {
		t.Fatal("first form normalized to empty")
	}
	for _, f := range forms[1:] {
		if got := NormalizeRemoteURL(f); got != want {
			t.Errorf("form %q = %q, want convergence to %q", f, got, want)
		}
	}
}
