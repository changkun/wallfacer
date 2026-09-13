package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartupRejectsUnavailableSecretsBeforeServing(t *testing.T) {
	if dir := os.Getenv("WALLFACER_TEST_SECRET_STARTUP"); dir != "" {
		sc := initServer(dir, ServerConfig{LogFormat: "text", Addr: "127.0.0.1:0", DataDir: filepath.Join(dir, "data"), EnvFile: filepath.Join(dir, ".env")}, testFS(t), testFS(t))
		defer sc.Shutdown()
		t.Fatal("server started with unresolved credentials and potentially disabled cloud authentication")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("WALLFACER_CLOUD=true\nWALLFACER_SECRET_STORE=keyring\nWALLFACER_SECRET_BUNDLE=invalid\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestStartupRejectsUnavailableSecretsBeforeServing$")
	cmd.Env = append(os.Environ(), "WALLFACER_TEST_SECRET_STARTUP="+dir)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "provider secret storage unavailable") {
		t.Fatalf("expected credential-specific startup failure: %v\n%s", err, out)
	}
}
