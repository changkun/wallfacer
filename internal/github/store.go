package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Principal is the owner key a stored [Token] is scoped to: the tenant (OrgID)
// and the canonical owner (Sub), mirroring authkit.Identity's tenancy pair so a
// signed-in user's GitHub token is never reused across principals.
type Principal struct {
	OrgID string
	Sub   string
}

// key derives a stable, filesystem-safe, low-cardinality identifier for a
// principal. The OrgID and Sub are joined with a NUL separator (which cannot
// appear in either) before hashing so distinct principals cannot collide.
func (p Principal) key() string {
	sum := sha256.Sum256([]byte(p.OrgID + "\x00" + p.Sub))
	return hex.EncodeToString(sum[:])
}

// Store persists a brokered GitHub [Token] per [Principal] between process
// runs. Implementations must be safe for read-modify-write loops (Save then
// Load returns the saved value) and must return (nil, nil) from Load when no
// token is stored for the principal.
type Store interface {
	// Save persists t for p. Disk-backed implementations restrict the
	// credential's permissions to the owning user.
	Save(ctx context.Context, p Principal, t *Token) error

	// Load returns the token stored for p, or (nil, nil) if none. A non-nil
	// error means the backend is reachable but unreadable (corrupted data,
	// permission denied), not "absent".
	Load(ctx context.Context, p Principal) (*Token, error)

	// Clear removes any token stored for p. Idempotent: clearing an absent
	// token returns nil.
	Clear(ctx context.Context, p Principal) error
}

// FileStore persists one credential file per principal under Dir. Files are
// written with mode 0600 and Dir is created with mode 0700 on first Save,
// matching the authkit file token store and common credential-file practice
// (gh, gcloud). The on-disk shape is the JSON encoding of [Token].
type FileStore struct {
	Dir string
}

// NewFileStore returns a FileStore writing credential files under dir. dir must
// be non-empty; it is created lazily on the first Save so a read-only
// environment can still Load without side effects.
func NewFileStore(dir string) (*FileStore, error) {
	if dir == "" {
		return nil, errors.New("github: FileStore dir is empty")
	}
	return &FileStore{Dir: dir}, nil
}

func (s *FileStore) path(p Principal) string {
	return filepath.Join(s.Dir, "github-"+p.key()+".json")
}

// Save writes t for p atomically: a sibling tempfile is written then renamed,
// so a partial write never truncates an existing credential.
func (s *FileStore) Save(_ context.Context, p Principal, t *Token) error {
	if t == nil {
		return errors.New("github: Save nil token")
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return fmt.Errorf("github: create token dir: %w", err)
	}
	data, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("github: marshal token: %w", err)
	}

	tmp, err := os.CreateTemp(s.Dir, ".github-token-*.tmp")
	if err != nil {
		return fmt.Errorf("github: create temp token: %w", err)
	}
	tmpPath := tmp.Name()
	cleaned := false
	defer func() {
		if !cleaned {
			_ = os.Remove(tmpPath)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("github: chmod temp token: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("github: write temp token: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("github: close temp token: %w", err)
	}
	if err := os.Rename(tmpPath, s.path(p)); err != nil {
		return fmt.Errorf("github: persist token: %w", err)
	}
	cleaned = true
	return nil
}

// Load returns the token stored for p, or (nil, nil) when the file is absent.
func (s *FileStore) Load(_ context.Context, p Principal) (*Token, error) {
	data, err := os.ReadFile(s.path(p))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("github: read token: %w", err)
	}
	var t Token
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("github: unmarshal token: %w", err)
	}
	return &t, nil
}

// Clear removes the credential file for p. A missing file is not an error.
func (s *FileStore) Clear(_ context.Context, p Principal) error {
	if err := os.Remove(s.path(p)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("github: clear token: %w", err)
	}
	return nil
}
