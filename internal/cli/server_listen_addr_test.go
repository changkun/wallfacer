package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitServer_AddrWithoutPortIsReported starts the server tree in a child
// process with a listen address that has no port. Startup must stop and name
// the address: read as an empty host, it would bind every interface on a
// fallback port where the address asked for one interface.
func TestInitServer_AddrWithoutPortIsReported(t *testing.T) {
	if dir := os.Getenv("WALLFACER_TEST_ADDR_WITHOUT_PORT"); dir != "" {
		sc := initServer(dir, ServerConfig{
			LogFormat: "text",
			Addr:      "127.0.0.1",
			DataDir:   filepath.Join(dir, "data"),
			EnvFile:   filepath.Join(dir, ".env"),
		}, testFS(t), testFS(t))
		defer sc.Shutdown()
		t.Fatalf("initServer accepted the listen address 127.0.0.1 and bound %s", sc.Ln.Addr())
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("# empty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestInitServer_AddrWithoutPortIsReported$")
	cmd.Env = append(os.Environ(), "WALLFACER_TEST_ADDR_WITHOUT_PORT="+dir)
	out, err := cmd.CombinedOutput()
	got := string(out)
	if err == nil || !strings.Contains(got, `fatal: listen: listen address "127.0.0.1"`) || !strings.Contains(got, "missing port in address") {
		t.Fatalf("want startup to stop with an error naming the listen address: %v\n%s", err, got)
	}
}

// TestBrowserURL covers the address `wallfacer run` opens in the browser: a
// wildcard or empty host opens localhost, a named host is kept (an IPv6 one in
// brackets), and an address without a port is an error naming it rather than
// a URL built from an empty host.
func TestBrowserURL(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{":8080", "http://localhost:4321"},
		{"0.0.0.0:8080", "http://localhost:4321"},
		{"[::]:8080", "http://localhost:4321"},
		{"127.0.0.1:8080", "http://127.0.0.1:4321"},
		{"[::1]:8080", "http://[::1]:4321"},
		{"board.example:8080", "http://board.example:4321"},
	}
	for _, tc := range tests {
		got, err := browserURL(tc.addr, 4321)
		if err != nil || got != tc.want {
			t.Errorf("browserURL(%q) = %q, %v; want %q", tc.addr, got, err, tc.want)
		}
	}
	for _, addr := range []string{"127.0.0.1", "localhost", ""} {
		got, err := browserURL(addr, 4321)
		if err == nil || !strings.Contains(err.Error(), "listen address") {
			t.Errorf("browserURL(%q) = %q, %v; want an error naming the listen address", addr, got, err)
		}
	}
}
