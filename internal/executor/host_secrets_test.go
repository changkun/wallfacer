//go:build !windows

package executor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"

	"latere.ai/x/wallfacer/internal/envconfig"
)

func TestHostKeyringCredentialsRefreshBetweenLaunches(t *testing.T) {
	keyring.MockInit()
	bin := buildFakeAgent(t, "fakeagent")
	b, err := NewHostBackend(HostBackendConfig{ClaudeBinary: bin, CodexBinary: bin, OpenCodeBinary: bin, AgentNice: -1})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("ANTHROPIC_API_KEY=first-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mode := "keyring"
	if err := envconfig.Update(path, envconfig.Updates{SecretStore: &mode}); err != nil {
		t.Fatal(err)
	}
	spec := ContainerSpec{Name: "secret-refresh", EnvFile: path, Env: map[string]string{"WALLFACER_AGENT": "claude"}, Cmd: []string{"-p", "hi"}, WorkDir: t.TempDir()}
	for _, token := range []string{"first-secret", "second-secret"} {
		if err := envconfig.Update(path, envconfig.Updates{APIKey: &token}); err != nil {
			t.Fatal(err)
		}
		got := launchAndDrain(t, b, spec)
		echo, _ := got["env_echo"].(map[string]any)
		if echo["ANTHROPIC_API_KEY"] != token {
			t.Fatal("agent received stale or unresolved credentials")
		}
	}
	keyring.MockInitWithError(errors.New("locked"))
	t.Setenv("ANTHROPIC_API_KEY", "unrelated-inherited-token")
	for _, agent := range []string{"claude", "codex", "opencode"} {
		spec.Env["WALLFACER_AGENT"] = agent
		if _, err := b.Launch(context.Background(), spec); !errors.Is(err, envconfig.ErrSecretStore) {
			t.Fatalf("%s launched with inaccessible credentials: %v", agent, err)
		}
	}
}
