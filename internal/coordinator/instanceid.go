package coordinator

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// instanceIDFile is the per-data-dir file holding the stable instance id.
const instanceIDFile = "instance-id"

// LoadOrCreateInstanceID returns the instance's stable coordination id,
// generating and persisting one at <configDir>/instance-id on first use. The id
// survives a restart so a reconnect re-takes the same registry slot instead of
// appearing as a new instance (no presence flap, no dangling remote-control
// target). This is the same load-or-create pattern as the public-client cookie
// key.
//
// Two instances with different --data dirs get different ids (correct
// disambiguation, each has its own file); two sharing a data dir is unsupported
// and already disallowed by the store.
func LoadOrCreateInstanceID(configDir string) (string, error) {
	path := filepath.Join(configDir, instanceIDFile)
	if b, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(b)); validInstanceID(id) {
			return id, nil
		}
	}
	raw := make([]byte, 16)
	if _, err := cryptorand.Read(raw); err != nil {
		return "", err
	}
	id := "inst_" + hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(id), 0o600); err != nil {
		return "", fmt.Errorf("persist instance id: %w", err)
	}
	return id, nil
}

// validInstanceID guards against a truncated or hand-edited file: an id must
// carry the prefix and a non-trivial body.
func validInstanceID(id string) bool {
	return strings.HasPrefix(id, "inst_") && len(id) >= len("inst_")+8
}
