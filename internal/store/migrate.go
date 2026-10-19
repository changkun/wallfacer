package store

import (
	"encoding/json"
	"maps"
	"time"

	"latere.ai/x/wallfacer/internal/constants"
	"latere.ai/x/wallfacer/internal/harness"
)

// migrateTaskJSON deserializes raw JSON into a Task and applies any
// missing-value defaults, canonicalization, and schema-version stamping.
// It returns the migrated Task, whether any change was made (so the caller
// can persist the result and avoid redundant writes), and any parse error.
//
// Migration steps applied in order:
//  1. Migrate the deprecated Model field to ModelOverride (when Model is set
//     and ModelOverride is unset), then clear Model.
//  2. Default missing/zero values: Status → "backlog", Timeout via
//     clampTimeout, missing CreatedAt/UpdatedAt from file mod time.
//  3. Normalize Sandbox (trim) and SandboxByActivity via
//     normalizeSandboxByActivity.
//  4. Backfill AutoRetryBudget for tasks created before schema version 2.
//  5. Stamp SchemaVersion = constants.CurrentTaskSchemaVersion.
//
// Keys a stored record carries that Task does not declare, such as an older
// record's depends_on list, are dropped by json.Unmarshal and disappear on
// the record's next save.
func migrateTaskJSON(raw []byte, fileModTime time.Time) (Task, bool, error) {
	var task Task
	if err := json.Unmarshal(raw, &task); err != nil {
		return Task{}, false, err
	}

	changed := false

	// Migrate deprecated Model field to ModelOverride.
	if task.Model != "" && task.ModelOverride == nil {
		task.ModelOverride = &task.Model
		task.Model = ""
		changed = true
	}

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

	// (2) Normalize Sandbox and SandboxByActivity.
	if normalSandbox := harness.NormalizeID(string(task.Sandbox)); normalSandbox != task.Sandbox {
		task.Sandbox = normalSandbox
		changed = true
	}
	if normalSBA := normalizeSandboxByActivity(task.SandboxByActivity); !maps.Equal(normalSBA, task.SandboxByActivity) {
		task.SandboxByActivity = normalSBA
		changed = true
	}

	// (3) Backfill AutoRetryBudget for tasks created before schema version 2.
	// Only transient/infrastructure failures get a budget; agent_error and
	// timeout are not retried automatically because they are likely to recur.
	if task.AutoRetryBudget == nil {
		task.AutoRetryBudget = map[FailureCategory]int{
			FailureCategoryContainerCrash: 2,
			FailureCategorySyncError:      2,
			FailureCategoryWorktree:       1,
		}
		changed = true
	}

	// (4) Guarantee SchemaVersion is current.
	if task.SchemaVersion != constants.CurrentTaskSchemaVersion {
		task.SchemaVersion = constants.CurrentTaskSchemaVersion
		changed = true
	}

	return task, changed, nil
}
