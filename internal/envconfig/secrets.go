package envconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/zalando/go-keyring"
	"latere.ai/x/pkg/atomicfile"
)

const secretService = "wallfacer"
const secretModeKey = "WALLFACER_SECRET_STORE"
const secretBundleKey = "WALLFACER_SECRET_BUNDLE"

// ErrSecretStore marks a configuration error that must prevent agent launch.
// Keyring errors are deliberately not wrapped: platform errors can include
// command input, which may contain credentials.
var ErrSecretStore = errors.New("provider secret storage unavailable")

var envMu sync.RWMutex
var writeEnvFile = atomicfile.Write

var providerSecretKeys = []string{
	"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN",
	"OPENAI_API_KEY", "CURSOR_API_KEY", "OPENCODE_SERVER_PASSWORD",
}

type secretBundle struct {
	Version int               `json:"version"`
	Values  map[string]string `json:"values"`
}

func rawValues(raw []byte) map[string]string {
	values := make(map[string]string)
	for line := range strings.SplitSeq(string(raw), "\n") {
		if k, v, ok := parseEnvLine(line); ok {
			values[k] = v
		}
	}
	return values
}

func storageMode(values map[string]string) (string, error) {
	mode := values[secretModeKey]
	if mode == "" {
		mode = "file"
	}
	if mode != "file" && mode != "keyring" {
		return "", fmt.Errorf("%w: choose file or keyring", ErrSecretStore)
	}
	return mode, nil
}

func resolveSecrets(values map[string]string) error {
	mode, err := storageMode(values)
	if err != nil {
		return err
	}
	if mode == "file" {
		return nil
	}
	ref := values[secretBundleKey]
	if _, err := uuid.Parse(ref); err != nil {
		return fmt.Errorf("%w: invalid credential reference", ErrSecretStore)
	}
	encoded, err := keyring.Get(secretService, ref)
	if err != nil {
		return fmt.Errorf("%w: unlock the system keyring and restore the credential bundle", ErrSecretStore)
	}
	var bundle secretBundle
	if err := json.Unmarshal([]byte(encoded), &bundle); err != nil || bundle.Version != 1 || bundle.Values == nil {
		return fmt.Errorf("%w: invalid credential bundle", ErrSecretStore)
	}
	for _, k := range providerSecretKeys {
		values[k] = bundle.Values[k]
	}
	return nil
}

// readConfigFile resolves provider credentials without serializing them back
// through env syntax, which would corrupt passwords containing quotes or '#'.
func readConfigFile(path string) ([]byte, map[string]string, error) {
	envMu.RLock()
	defer envMu.RUnlock()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	values := rawValues(raw)
	if err := resolveSecrets(values); err != nil {
		return nil, nil, err
	}
	return raw, values, nil
}

// updateSecrets stages and verifies a fresh bundle before atomically publishing
// its reference. Existing credentials remain usable if either write fails.
// The caller holds envMu. clone preserves the source bundle for test sessions.
func updateSecrets(path string, raw []byte, updates map[string]*string, clone bool) error {
	values := rawValues(raw)
	oldMode, err := storageMode(values)
	if err != nil {
		return err
	}
	if err := resolveSecrets(values); err != nil {
		return err
	}
	oldRef := values[secretBundleKey]
	mode := oldMode
	if v := updates[secretModeKey]; v != nil {
		mode = *v
	}
	if mode != "file" && mode != "keyring" {
		return fmt.Errorf("%w: choose file or keyring", ErrSecretStore)
	}
	changed := clone || mode != oldMode
	for _, k := range providerSecretKeys {
		if v := updates[k]; v != nil {
			changed = changed || values[k] != *v
			values[k] = *v
		}
	}
	newRef := oldRef
	if mode == "keyring" {
		if changed {
			bundle := secretBundle{Version: 1, Values: make(map[string]string)}
			for _, k := range providerSecretKeys {
				if values[k] != "" {
					bundle.Values[k] = values[k]
				}
			}
			encoded, err := json.Marshal(bundle)
			if err != nil {
				return fmt.Errorf("%w: encode credentials", ErrSecretStore)
			}
			newRef = uuid.NewString()
			if err := keyring.Set(secretService, newRef, string(encoded)); err != nil {
				_ = keyring.Delete(secretService, newRef)
				return fmt.Errorf("%w: could not save; unlock the system keyring", ErrSecretStore)
			}
			if saved, err := keyring.Get(secretService, newRef); err != nil || saved != string(encoded) {
				_ = keyring.Delete(secretService, newRef)
				return fmt.Errorf("%w: credential verification failed", ErrSecretStore)
			}
		}
		for _, k := range providerSecretKeys {
			empty := ""
			updates[k] = &empty
		}
		updates[secretBundleKey] = &newRef
	} else if oldMode == "keyring" {
		for _, k := range providerSecretKeys {
			v := values[k]
			updates[k] = &v
		}
		empty := ""
		updates[secretBundleKey] = &empty
	}
	if err := writeEnvFile(path, renderUpdates(raw, updates), 0600); err != nil {
		if newRef != oldRef {
			_ = keyring.Delete(secretService, newRef)
		}
		return fmt.Errorf("write env file: %w", err)
	}
	if !clone && oldMode == "keyring" && (newRef != oldRef || mode != oldMode) {
		// A failed deletion leaves an unreferenced credential, never a broken
		// active configuration. Some keyrings disallow deletion while locked.
		_ = keyring.Delete(secretService, oldRef)
	}
	return nil
}

// CloneForTest copies settings into an existing temporary file, giving keyring
// credentials their own reference so overrides cannot change the saved bundle.
func CloneForTest(source, destination string) error {
	envMu.Lock()
	defer envMu.Unlock()
	raw, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return updateSecrets(destination, raw, make(map[string]*string), true)
}

// RemoveTestFile removes the temporary file and its independently owned bundle.
// Only pass a file created by CloneForTest or a new standalone temporary file.
func RemoveTestFile(path string) {
	envMu.Lock()
	defer envMu.Unlock()
	if raw, err := os.ReadFile(path); err == nil {
		values := rawValues(raw)
		if values[secretModeKey] == "keyring" {
			_ = keyring.Delete(secretService, values[secretBundleKey])
		}
	}
	_ = os.Remove(path)
}
