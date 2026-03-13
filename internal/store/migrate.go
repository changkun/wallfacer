package store

import (
	"encoding/json"
	"sort"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/sandbox"
	"github.com/google/uuid"
)

// migrateTaskJSON deserializes raw JSON into a Task and applies any
// missing-value defaults, canonicalization, and schema-version stamping.
// It returns the migrated Task, whether any change was made (so the caller
// can persist the result and avoid redundant writes), and any parse error.
//
// Migration steps applied in order:
//  1. Default missing/zero values: Status → "backlog", Timeout via
//     clampTimeout, missing CreatedAt/UpdatedAt from file mod time.
//  2. Canonicalize DependsOn: trim whitespace, UUID-validate, deduplicate,
//     stable-sort.
//  3. Normalize Sandbox (trim) and SandboxByActivity via
//     normalizeSandboxByActivity.
//  4. Backfill AutoRetryBudget for tasks created before schema version 2.
//  5. Stamp SchemaVersion = CurrentTaskSchemaVersion.
func migrateTaskJSON(raw []byte, fileModTime time.Time) (Task, bool, error) {
	var task Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return Task{}, false, err
	}

	changed := false

	// (1) Default missing/zero values.
	if task.Status == "" {
		task.Status = TaskStatusBacklog
		changed = true
	}
	if task.Timeout == 0 {
		task.Timeout = clampTimeout(0) // returns 60 (the default)
		changed = true
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = fileModTime
		changed = true
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = fileModTime
		changed = true
	}

	// (2) Canonicalize DependsOn.
	if len(task.DependsOn) > 0 {
		canon := canonicalizeDependsOn(task.DependsOn)
		if !stringSliceEqual(canon, task.DependsOn) {
			task.DependsOn = canon
			changed = true
		}
	}

	// (3) Normalize Sandbox and SandboxByActivity.
	if normalSandbox := sandbox.Normalize(string(task.Sandbox)); normalSandbox != task.Sandbox {
		task.Sandbox = normalSandbox
		changed = true
	}
	if normalSBA := normalizeSandboxByActivity(task.SandboxByActivity); !sandboxByActivityEqual(normalSBA, task.SandboxByActivity) {
		task.SandboxByActivity = normalSBA
		changed = true
	}

	// (4) Backfill AutoRetryBudget for tasks created before schema version 2.
	if task.AutoRetryBudget == nil {
		task.AutoRetryBudget = map[FailureCategory]int{
			FailureCategoryContainerCrash: 2,
			FailureCategorySyncError:      2,
			FailureCategoryWorktree:       1,
		}
		changed = true
	}

	// (5) Guarantee SchemaVersion is current.
	if task.SchemaVersion != CurrentTaskSchemaVersion {
		task.SchemaVersion = CurrentTaskSchemaVersion
		changed = true
	}

	return task, changed, nil
}

// canonicalizeDependsOn trims whitespace from each element, validates UUID
// format (dropping non-UUID values), deduplicates using the 16-byte UUID value
// (so case and format variants are unified), and sorts the result in ascending
// order. Returns nil instead of an empty slice so json:"omitempty" keeps the
// field absent in JSON when there are no valid dependencies.
func canonicalizeDependsOn(deps []string) []string {
	seen := make(map[uuid.UUID]struct{}, len(deps))
	out := make([]string, 0, len(deps))
	for _, dep := range deps {
		id, err := uuid.Parse(strings.TrimSpace(dep))
		if err != nil {
			continue // drop non-UUID values
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id.String()) // canonical lowercase hyphenated form
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// stringSliceEqual reports whether a and b are identical element-by-element.
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sandboxByActivityEqual reports whether two sandbox-by-activity maps contain
// exactly the same keys and values.
func sandboxByActivityEqual(a, b map[string]sandbox.Type) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
