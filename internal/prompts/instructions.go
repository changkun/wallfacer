package prompts

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
)

const (
	// CodexInstructionsFilename is the per-repo instructions filename agents read under the Codex sandbox.
	CodexInstructionsFilename = "AGENTS.md"
	// ClaudeInstructionsFilename is the per-repo instructions filename agents read under the Claude sandbox.
	ClaudeInstructionsFilename = "CLAUDE.md"
)

// WorkspaceDataKey returns a stable 16-char hex key derived from a set of
// workspace folder paths. The key is the SHA-256 of the sorted, colon-joined
// absolute paths, so the same set of folders always maps to the same key
// regardless of order.
//
// This is the path-seeded key the workspace model uses ONLY when migrating
// legacy workspace groups (whose data directories are already named by this
// hash) so migration moves zero bytes. New workspaces get a random key from
// [NewDataKey] instead, decoupling storage identity from the folder set.
func WorkspaceDataKey(workspaces []string) string {
	sorted := slices.Clone(workspaces)
	slices.Sort(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, ":")))
	return fmt.Sprintf("%x", h[:8]) // 16 hex chars
}

// NewDataKey returns a fresh random 16-char hex storage key for a newly
// created workspace. Because it is independent of the folder set, two
// workspaces pointing at the same folders get distinct storage (and distinct
// history), and a new workspace never inherits a migrated workspace's data.
func NewDataKey() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is unrecoverable for key generation.
		panic(fmt.Sprintf("prompts: generate data key: %v", err))
	}
	return hex.EncodeToString(b[:]) // 16 hex chars, matches WorkspaceDataKey width
}
