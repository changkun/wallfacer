package store

import (
	"os"
	"path/filepath"
	"time"

	"changkun.de/x/wallfacer/internal/pkg/ndjson"
	"changkun.de/x/wallfacer/internal/prompts"
)

// PlanningGroupKey returns the path-safe fingerprint used to scope a
// planning usage log to a workspace group. It matches the scheme used by
// workspace AGENTS.md (internal/prompts) so the same set of workspaces
// always resolves to the same planning directory regardless of order.
func PlanningGroupKey(workspaces []string) string {
	return prompts.InstructionsKey(workspaces)
}

// PlanningUsageDir returns the directory that holds the planning usage
// log for the given group key under the store root.
func PlanningUsageDir(root, groupKey string) string {
	return filepath.Join(root, "planning", groupKey)
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
