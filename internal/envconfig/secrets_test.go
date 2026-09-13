package envconfig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zalando/go-keyring"
)

func secretFixture(t *testing.T) string {
	t.Helper()
	keyring.MockInit()
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("ANTHROPIC_API_KEY=original-secret\nWALLFACER_CLOUD=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestKeyringUpdatesAndExplicitFileMigration(t *testing.T) {
	path := secretFixture(t)
	mode, token := "keyring", "new-secret"
	if err := Update(path, Updates{SecretStore: &mode}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	oldRef := rawValues(before)[secretBundleKey]
	if err := Update(path, Updates{APIKey: &token}); err != nil {
		t.Fatal(err)
	}
	if _, err := keyring.Get(secretService, oldRef); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatal("superseded bundle retained")
	}
	if err := UpdateSandboxSettings(path, nil, nil); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(path)
	if err != nil || cfg.APIKey != token || !cfg.Cloud {
		t.Fatalf("update lost settings: %v", err)
	}
	mode = "file"
	if err := Update(path, Updates{SecretStore: &mode}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if !strings.Contains(string(raw), "ANTHROPIC_API_KEY="+token) || strings.Contains(string(raw), secretBundleKey) {
		t.Fatal("explicit file migration failed")
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0600 {
		t.Fatal("credential file must be private")
	}
}

func TestKeyringFailuresPreserveOriginalConfiguration(t *testing.T) {
	for _, failure := range []string{"keyring-write", "file-write", "locked", "malformed", "invalid-reference", "invalid-mode"} {
		t.Run(failure, func(t *testing.T) {
			path := secretFixture(t)
			mode, token := "keyring", "replacement-secret"
			if failure == "locked" || failure == "malformed" || failure == "invalid-reference" {
				if err := Update(path, Updates{SecretStore: &mode}); err != nil {
					t.Fatal(err)
				}
				raw, _ := os.ReadFile(path)
				ref := rawValues(raw)[secretBundleKey]
				switch failure {
				case "locked":
					keyring.MockInitWithError(errors.New("locked replacement-secret"))
				case "malformed":
					_ = keyring.Set(secretService, ref, `{"version":2}`)
				case "invalid-reference":
					_ = os.WriteFile(path, []byte("WALLFACER_SECRET_STORE=keyring\nWALLFACER_SECRET_BUNDLE=invalid\n"), 0600)
				}
				if _, err := Parse(path); !errors.Is(err, ErrSecretStore) {
					t.Fatalf("expected explicit read failure: %v", err)
				}
				if _, err := ReadRaw(path); !errors.Is(err, ErrSecretStore) {
					t.Fatalf("raw reader hid failure: %v", err)
				}
			}
			if failure == "keyring-write" {
				keyring.MockInitWithError(errors.New("keyring error replacement-secret"))
			}
			if failure == "file-write" {
				originalWrite := writeEnvFile
				writeEnvFile = func(string, []byte, os.FileMode) error { return errors.New("disk full") }
				t.Cleanup(func() { writeEnvFile = originalWrite })
			}
			if failure == "invalid-mode" {
				mode = "invalid"
			}
			before, _ := os.ReadFile(path)
			err := Update(path, Updates{SecretStore: &mode, APIKey: &token})
			if err == nil {
				t.Fatal("expected update failure")
			}
			if strings.Contains(err.Error(), token) {
				t.Fatal("error disclosed a credential")
			}
			after, _ := os.ReadFile(path)
			if string(after) != string(before) {
				t.Fatal("failed update modified original file")
			}
		})
	}
}

func TestKeyringTestOverridesAreIndependent(t *testing.T) {
	path := secretFixture(t)
	mode := "keyring"
	if err := Update(path, Updates{SecretStore: &mode}); err != nil {
		t.Fatal(err)
	}
	temporary := filepath.Join(t.TempDir(), "test.env")
	if err := CloneForTest(path, temporary); err != nil {
		t.Fatal(err)
	}
	token := "test-secret"
	if err := Update(temporary, Updates{APIKey: &token}); err != nil {
		t.Fatal(err)
	}
	saved, err := Parse(path)
	if err != nil || saved.APIKey != "original-secret" {
		t.Fatal("test override changed saved credentials")
	}
	tested, err := Parse(temporary)
	if err != nil || tested.APIKey != token {
		t.Fatal("test override not applied")
	}
	raw, _ := os.ReadFile(temporary)
	ref := rawValues(raw)[secretBundleKey]
	RemoveTestFile(temporary)
	if _, err := os.Stat(temporary); !os.IsNotExist(err) {
		t.Fatal("test file retained")
	}
	if _, err := keyring.Get(secretService, ref); !errors.Is(err, keyring.ErrNotFound) {
		t.Fatal("temporary bundle retained")
	}
	if _, err := Parse(path); err != nil {
		t.Fatal("cleanup broke saved credentials")
	}
}

func TestKeyringConcurrentUpdatesPreserveFields(t *testing.T) {
	path := secretFixture(t)
	mode := "keyring"
	if err := Update(path, Updates{SecretStore: &mode}); err != nil {
		t.Fatal(err)
	}
	a, b, c := "claude-new", "openai-new", "cursor-new"
	var wg sync.WaitGroup
	for _, u := range []Updates{{APIKey: &a}, {OpenAIAPIKey: &b}, {CursorAPIKey: &c}} {
		wg.Go(func() {
			if err := Update(path, u); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	cfg, err := Parse(path)
	if err != nil || cfg.APIKey != a || cfg.OpenAIAPIKey != b || cfg.CursorAPIKey != c {
		t.Fatalf("concurrent updates lost a field: %v", err)
	}
}

func TestKeyringMigrationRemovesPlaintext(t *testing.T) {
	keyring.MockInit()
	path := filepath.Join(t.TempDir(), ".env")
	original := "# keep this comment\nexport ANTHROPIC_API_KEY='secret-first'\nANTHROPIC_API_KEY=secret-final\nOPENAI_API_KEY=secret-openai\nANTHROPIC_AUTH_TOKEN=secret-gateway\nUNRELATED=keep\n"
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	mode := "keyring"
	if err := updateFile(path, map[string]*string{"WALLFACER_SECRET_STORE": &mode}); err != nil {
		t.Fatal(err)
	}
	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(onDisk), "secret-") {
		t.Fatal("provider credentials remain in plaintext after selecting keyring")
	}
	if !strings.Contains(string(onDisk), "# keep this comment") || !strings.Contains(string(onDisk), "UNRELATED=keep") {
		t.Fatal("migration lost unrelated settings")
	}
	cfg, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIKey != "secret-final" || cfg.OpenAIAPIKey != "secret-openai" || cfg.AuthToken != "secret-gateway" {
		t.Fatal("typed reader did not resolve saved credentials")
	}
	raw, err := ReadRaw(path)
	if err != nil {
		t.Fatal(err)
	}
	if raw["ANTHROPIC_API_KEY"] != cfg.APIKey {
		t.Fatal("raw and typed readers disagree")
	}
}

func TestKeyringPreservesPasswordCharacters(t *testing.T) {
	path := secretFixture(t)
	mode, token := "keyring", "password with # hash and 'quotes'"
	if err := Update(path, Updates{SecretStore: &mode, APIKey: &token}); err != nil {
		t.Fatal(err)
	}
	cfg, err := Parse(path)
	if err != nil || cfg.APIKey != token {
		t.Fatal("typed resolution changed password characters")
	}
	raw, err := ReadRaw(path)
	if err != nil || raw["ANTHROPIC_API_KEY"] != token {
		t.Fatal("host resolution changed password characters")
	}
}

func TestCredentialStorageRoundTripPreservesSpecialCharacters(t *testing.T) {
	for _, initialMode := range []string{"file", "keyring"} {
		t.Run(initialMode, func(t *testing.T) {
			path := secretFixture(t)
			mode := initialMode
			token := "  password # 'single' \"double\" \\path\nsecond line\t "
			if err := Update(path, Updates{SecretStore: &mode, APIKey: &token}); err != nil {
				t.Fatal(err)
			}
			for _, mode = range []string{"keyring", "file"} {
				if err := Update(path, Updates{SecretStore: &mode}); err != nil {
					t.Fatal(err)
				}
				cfg, err := Parse(path)
				if err != nil || cfg.APIKey != token {
					t.Fatalf("%s migration changed credential characters: %v", mode, err)
				}
				raw, err := ReadRaw(path)
				if err != nil || raw["ANTHROPIC_API_KEY"] != token {
					t.Fatalf("%s host reader changed credential characters: %v", mode, err)
				}
			}
		})
	}
}
