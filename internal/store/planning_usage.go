package store

import (
	"os"
	"path/filepath"
	"time"

	"latere.ai/x/wallfacer/internal/pkg/ndjson"
	"latere.ai/x/wallfacer/internal/prompts"
)

// PlanningGroupKey returns the path-safe fingerprint used to scope a
// planning usage log to a workspace group. It matches the scheme used by
// workspace AGENTS.md (internal/prompts) so the same set of workspaces
// always resolves to the same planning directory regardless of order.
func PlanningGroupKey(workspaces []string) string {
	return prompts.InstructionsKey(workspaces)
}

// agentSessionsDirName is the per-root directory that holds agent-session
// state (chat threads + usage logs), one subdirectory per workspace-group
// fingerprint. It was historically named "planning"; MigrateAgentSessionsDir
// renames the old layout on startup. Keep in sync with the same constant in
// internal/agentsession (which stays free of a store dependency).
const agentSessionsDirName = "agent-sessions"

// legacyPlanningDirName is the pre-rename directory name, migrated away from
// by MigrateAgentSessionsDir.
const legacyPlanningDirName = "planning"

// AgentSessionsRoot returns the directory holding all agent-session state
// under the store root.
func AgentSessionsRoot(root string) string {
	return filepath.Join(root, agentSessionsDirName)
}

// PlanningUsageDir returns the directory that holds the agent-session usage
// log for the given group key under the store root.
func PlanningUsageDir(root, groupKey string) string {
	return filepath.Join(AgentSessionsRoot(root), groupKey)
}

// MigrateAgentSessionsDir renames a pre-existing <root>/planning directory to
// <root>/agent-sessions when the new layout is absent. It is idempotent (a
// no-op once migrated or when there is nothing to move) and reports whether a
// rename happened. Call once at startup before any agent-session path is read.
func MigrateAgentSessionsDir(root string) (bool, error) {
	if root == "" {
		return false, nil
	}
	oldDir := filepath.Join(root, legacyPlanningDirName)
	newDir := filepath.Join(root, agentSessionsDirName)
	if _, err := os.Stat(newDir); err == nil {
		return false, nil // already migrated
	}
	if _, err := os.Stat(oldDir); err != nil {
		return false, nil // nothing to migrate
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return false, err
	}
	return true, nil
}

// PlanningUsagePath returns the NDJSON file path that records per-round
// planning usage for the given group key.
func PlanningUsagePath(root, groupKey string) string {
	return filepath.Join(PlanningUsageDir(root, groupKey), "usage.jsonl")
}

// AppendPlanningUsage appends a single TurnUsageRecord to the planning
// usage log for the given group key. The enclosing directory is created
// on demand. Each append is a single small write and is atomic on common
// Linux filesystems.
func AppendPlanningUsage(root, groupKey string, rec TurnUsageRecord) error {
	if err := os.MkdirAll(PlanningUsageDir(root, groupKey), 0755); err != nil {
		return err
	}
	return ndjson.AppendFile(PlanningUsagePath(root, groupKey), rec)
}

// ReadPlanningUsage returns the planning usage records for the given
// group key whose Timestamp is strictly after since. When since is the
// zero time all records are returned. A missing log yields (nil, nil).
func ReadPlanningUsage(root, groupKey string, since time.Time) ([]TurnUsageRecord, error) {
	path := PlanningUsagePath(root, groupKey)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	all, err := ndjson.ReadFile[TurnUsageRecord](path)
	if err != nil {
		return nil, err
	}
	if since.IsZero() {
		return all, nil
	}
	filtered := make([]TurnUsageRecord, 0, len(all))
	for _, rec := range all {
		if rec.Timestamp.After(since) {
			filtered = append(filtered, rec)
		}
	}
	return filtered, nil
}
