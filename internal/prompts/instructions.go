package prompts

import (
	"crypto/sha256"
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

// InstructionsKey returns a stable 16-char hex key for a given set of workspace paths.
// The key is derived from the SHA-256 of the sorted, colon-joined absolute paths,
// so the same set of workspaces always maps to the same key regardless of order.
func InstructionsKey(workspaces []string) string {
	sorted := slices.Clone(workspaces)
	slices.Sort(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, ":")))
	return fmt.Sprintf("%x", h[:8]) // 16 hex chars
}
