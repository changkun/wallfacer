// Package apicontract is the single source of truth for all HTTP API routes.
//
// Routes is the canonical list used to:
//   - Register handlers in the HTTP multiplexer (server.go buildMux).
//   - Generate the frontend route helpers (ui/js/generated/routes.js).
//   - Generate the machine-readable API contract (docs/internals/api-contract.json).
//
// To regenerate derived artifacts after editing Routes, run:
//
//	make api-contract
//
// Tests in server_routes_test.go assert that every route in Routes is actually
// registered in the mux, and that the generated artifacts are not stale.
package apicontract

import "net/http"

// Route describes a single HTTP API endpoint.
type Route struct {
	// Method is the HTTP verb: GET, POST, PUT, PATCH, or DELETE.
	Method string
	// Pattern is the URL pattern accepted by http.ServeMux (may contain {id}, {filename}).
	Pattern string
	// Name is the unique Go handler method name in internal/handler (e.g. "ListTasks").
	Name string
	// JSName is the JavaScript method name emitted in routes.js. When empty the
	// generator derives it from the URL path suffix (kebab-and-slash to camelCase).
	// Set it explicitly only when auto-derivation would be ambiguous (e.g. two routes
	// share the same URL but differ by HTTP method).
	JSName string
	// Description is a short human-readable summary of what the route does.
	Description string
	// Tags are logical group labels used for documentation and filtering.
	Tags []string
}

// FullPattern returns the combined "METHOD /pattern" string expected by
// http.ServeMux.HandleFunc (Go 1.22+ syntax).
func (r Route) FullPattern() string {
	return r.Method + " " + r.Pattern
}

// Routes is the single source of truth for all HTTP API endpoints.
// The order here determines the order in generated artifacts.
var Routes = []Route{

	// --- Debug & monitoring ---

	{
		Method: http.MethodGet, Pattern: "/api/debug/health", Name: "Health",
		Description: "Operational health check: goroutine count, task counts, uptime.",
		Tags:        []string{"debug"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/debug/spans", Name: "GetSpanStats",
		Description: "Aggregate span timing statistics across all tasks.",
		Tags:        []string{"debug"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/debug/runtime", Name: "GetRuntimeStatus",
		Description: "Live server internals: pending goroutines, memory, task states, containers.",
		Tags:        []string{"debug"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/debug/board", Name: "BoardManifest",
		Description: "Board manifest as seen by a hypothetical new task (no self-task, no worktree mounts).",
		Tags:        []string{"debug"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/{id}/board", Name: "TaskBoardManifest",
		Description: "Board manifest as it appeared to a specific task (is_self=true, MountWorktrees applied).",
		Tags:        []string{"tasks", "debug"},
	},

	// --- Container monitoring ---

	{
		Method: http.MethodGet, Pattern: "/api/containers", Name: "GetContainers",
		JSName:      "list",
		Description: "List running sandbox containers.",
		Tags:        []string{"containers"},
	},

	// --- Sandbox image management ---

	{
		Method: http.MethodGet, Pattern: "/api/images", Name: "GetImageStatus",
		JSName:      "status",
		Description: "Check which sandbox images are cached locally, with size and creation time.",
		Tags:        []string{"images"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/images/pull", Name: "PullImage",
		JSName:      "pull",
		Description: "Start an async pull for a sandbox image; returns a pull ID.",
		Tags:        []string{"images"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/images", Name: "DeleteImage",
		JSName:      "remove",
		Description: "Remove a cached sandbox image.",
		Tags:        []string{"images"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/images/pull/stream", Name: "StreamImagePull",
		JSName:      "pullStream",
		Description: "SSE stream of pull progress for a given pull ID.",
		Tags:        []string{"images"},
	},

	// --- File listing ---

	{
		Method: http.MethodGet, Pattern: "/api/files", Name: "GetFiles",
		JSName:      "list",
		Description: "File listing for @ mention autocomplete.",
		Tags:        []string{"files"},
	},

	// --- Server configuration ---

	{
		Method: http.MethodGet, Pattern: "/api/config", Name: "GetConfig",
		JSName:      "get",
		Description: "Get server configuration (workspaces, autopilot flags, sandbox list).",
		Tags:        []string{"config"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/config", Name: "UpdateConfig",
		JSName:      "update",
		Description: "Update server configuration (autopilot, autotest, autosubmit, sandbox assignments).",
		Tags:        []string{"config"},
	},

	// --- Workspace selection ---

	{
		Method: http.MethodGet, Pattern: "/api/workspaces/browse", Name: "BrowseWorkspaces",
		Description: "List child directories for an absolute host path.",
		Tags:        []string{"workspaces"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/workspaces/mkdir", Name: "MkdirWorkspace",
		JSName:      "mkdir",
		Description: "Create a new directory under an absolute host path.",
		Tags:        []string{"workspaces"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/workspaces/rename", Name: "RenameWorkspace",
		JSName:      "rename",
		Description: "Rename a directory at an absolute host path.",
		Tags:        []string{"workspaces"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/workspaces", Name: "UpdateWorkspaces",
		JSName:      "update",
		Description: "Replace the active workspace set and switch the scoped task board.",
		Tags:        []string{"workspaces"},
	},

	// --- Ideation / brainstorm agent ---

	{
		Method: http.MethodGet, Pattern: "/api/ideate", Name: "GetIdeationStatus",
		JSName:      "status",
		Description: "Get brainstorm/ideation agent status.",
		Tags:        []string{"ideate"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/ideate", Name: "TriggerIdeation",
		JSName:      "trigger",
		Description: "Trigger the ideation agent to generate new task ideas.",
		Tags:        []string{"ideate"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/ideate", Name: "CancelIdeation",
		JSName:      "cancel",
		Description: "Cancel an in-progress ideation run.",
		Tags:        []string{"ideate"},
	},

	// --- Environment configuration ---

	{
		Method: http.MethodGet, Pattern: "/api/env", Name: "GetEnvConfig",
		JSName:      "get",
		Description: "Get environment configuration (tokens masked).",
		Tags:        []string{"env"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/env", Name: "UpdateEnvConfig",
		JSName:      "update",
		Description: "Update environment file; omitted/empty token fields are preserved.",
		Tags:        []string{"env"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/env/test", Name: "TestSandbox",
		Description: "Test sandbox configuration by running a lightweight probe task.",
		Tags:        []string{"env"},
	},
	// --- Workspace instructions ---

	{
		Method: http.MethodGet, Pattern: "/api/instructions", Name: "GetInstructions",
		JSName:      "get",
		Description: "Get the workspace CLAUDE.md content.",
		Tags:        []string{"instructions"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/instructions", Name: "UpdateInstructions",
		JSName:      "update",
		Description: "Save the workspace CLAUDE.md.",
		Tags:        []string{"instructions"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/instructions/reinit", Name: "ReinitInstructions",
		Description: "Rebuild workspace CLAUDE.md from default template and repo files.",
		Tags:        []string{"instructions"},
	},

	// --- System prompt templates (user-overridable built-in prompts) ---

	{
		Method: http.MethodGet, Pattern: "/api/system-prompts", Name: "ListSystemPrompts",
		JSName:      "list",
		Description: "List all 8 built-in system prompt templates with override status and content.",
		Tags:        []string{"system-prompts"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/system-prompts/{name}", Name: "GetSystemPrompt",
		JSName:      "get",
		Description: "Get a single built-in system prompt template by name.",
		Tags:        []string{"system-prompts"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/system-prompts/{name}", Name: "UpdateSystemPrompt",
		JSName:      "update",
		Description: "Write a user override for a built-in system prompt template; validates the template before writing.",
		Tags:        []string{"system-prompts"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/system-prompts/{name}", Name: "DeleteSystemPrompt",
		JSName:      "delete",
		Description: "Remove the user override for a built-in system prompt template, restoring the embedded default.",
		Tags:        []string{"system-prompts"},
	},

	// --- Prompt templates ---

	{
		Method: http.MethodGet, Pattern: "/api/templates", Name: "ListTemplates",
		JSName:      "list",
		Description: "List all prompt templates sorted by created_at descending.",
		Tags:        []string{"templates"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/templates", Name: "CreateTemplate",
		JSName:      "create",
		Description: "Create a new named prompt template.",
		Tags:        []string{"templates"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/templates/{id}", Name: "DeleteTemplate",
		JSName:      "delete",
		Description: "Delete a prompt template by ID.",
		Tags:        []string{"templates"},
	},

	// --- Git workspace operations ---

	{
		Method: http.MethodGet, Pattern: "/api/git/status", Name: "GitStatus",
		Description: "Git status for all mounted workspaces.",
		Tags:        []string{"git"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/git/stream", Name: "GitStatusStream",
		Description: "SSE stream of git status updates for all workspaces.",
		Tags:        []string{"git", "sse"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/git/push", Name: "GitPush",
		Description: "Push a workspace to its remote.",
		Tags:        []string{"git"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/git/sync", Name: "GitSyncWorkspace",
		Description: "Fetch and rebase a workspace onto its upstream branch.",
		Tags:        []string{"git"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/git/rebase-on-main", Name: "GitRebaseOnMain",
		Description: "Fetch origin/<main> and rebase the current branch on top.",
		Tags:        []string{"git"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/git/branches", Name: "GitBranches",
		Description: "List branches for a workspace.",
		Tags:        []string{"git"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/git/checkout", Name: "GitCheckout",
		Description: "Switch a workspace to a different branch.",
		Tags:        []string{"git"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/git/create-branch", Name: "GitCreateBranch",
		Description: "Create and check out a new branch in a workspace.",
		Tags:        []string{"git"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/git/open-folder", Name: "OpenFolder",
		Description: "Open a workspace directory in the OS file manager.",
		Tags:        []string{"git"},
	},

	// --- Usage & statistics ---

	{
		Method: http.MethodGet, Pattern: "/api/usage", Name: "GetUsageStats",
		JSName:      "stats",
		Description: "Aggregated token and cost usage statistics.",
		Tags:        []string{"stats"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/stats", Name: "GetStats",
		JSName:      "get",
		Description: "Task status and workspace cost statistics. Optional ?workspace=<repo-root-path> restricts aggregation to tasks for that workspace (400 if no tasks match).",
		Tags:        []string{"stats"},
	},

	// --- Task collection (no {id}) ---

	{
		Method: http.MethodGet, Pattern: "/api/tasks", Name: "ListTasks",
		JSName:      "list",
		Description: "List all tasks (optionally including archived).",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/stream", Name: "StreamTasks",
		Description: "SSE stream: full snapshot then incremental task-updated/task-deleted events.",
		Tags:        []string{"tasks", "sse"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks", Name: "CreateTask",
		JSName:      "create",
		Description: "Create a new task in the backlog.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/batch", Name: "BatchCreateTasks",
		JSName:      "batchCreate",
		Description: "Create multiple tasks atomically with symbolic dependency wiring.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/generate-titles", Name: "GenerateMissingTitles",
		Description: "Bulk-generate titles for tasks that lack one.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/generate-oversight", Name: "GenerateMissingOversight",
		Description: "Bulk-generate oversight summaries for eligible tasks.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/search", Name: "SearchTasks",
		Description: "Search tasks by keyword.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/archive-done", Name: "ArchiveAllDone",
		Description: "Archive all tasks in the done state.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/summaries", Name: "ListSummaries",
		JSName:      "summaries",
		Description: "List immutable task summaries for completed tasks (cost dashboard, no full task.json read).",
		Tags:        []string{"tasks", "stats"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/deleted", Name: "ListDeletedTasks",
		JSName:      "listDeleted",
		Description: "List soft-deleted (tombstoned) tasks that are within the retention window.",
		Tags:        []string{"tasks"},
	},

	// --- Task instance operations (require {id}) ---

	{
		Method: http.MethodPatch, Pattern: "/api/tasks/{id}", Name: "UpdateTask",
		JSName:      "update",
		Description: "Update task fields: status, prompt, timeout, sandbox, dependencies, fresh_start.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/tasks/{id}", Name: "DeleteTask",
		JSName:      "delete",
		Description: "Soft-delete a task (tombstone); data retained within retention window.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/{id}/events", Name: "GetEvents",
		Description: "Task event timeline (state changes, outputs, feedback, errors).",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/feedback", Name: "SubmitFeedback",
		Description: "Submit a feedback message to a waiting task.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/done", Name: "CompleteTask",
		Description: "Mark a waiting task as done and trigger commit-and-push.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/cancel", Name: "CancelTask",
		Description: "Cancel a task: kill container and discard worktrees.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/resume", Name: "ResumeTask",
		Description: "Resume a failed task using its existing session.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/restore", Name: "RestoreTask",
		Description: "Restore a soft-deleted task by removing its tombstone.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/archive", Name: "ArchiveTask",
		Description: "Move a done task to the archived state.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/unarchive", Name: "UnarchiveTask",
		Description: "Restore an archived task to the done state.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/sync", Name: "SyncTask",
		Description: "Rebase task worktrees onto the latest default branch.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/test", Name: "TestTask",
		Description: "Trigger the test agent for a task.",
		Tags:        []string{"tasks"},
	},

	{
		Method: http.MethodGet, Pattern: "/api/tasks/{id}/diff", Name: "TaskDiff",
		Description: "Git diff of task worktrees versus the default branch.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/{id}/logs", Name: "StreamLogs",
		Description: "SSE stream of live container logs for a running task.",
		Tags:        []string{"tasks", "sse"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/{id}/outputs/{filename}", Name: "ServeOutput",
		Description: "Raw Claude Code output file for a single agent turn.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/{id}/turn-usage", Name: "GetTurnUsage",
		Description: "Per-turn token usage breakdown for a task.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/{id}/spans", Name: "GetTaskSpans",
		Description: "Span timing statistics for a task.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/{id}/oversight", Name: "GetOversight",
		Description: "Oversight summary for a completed task.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/{id}/oversight/test", Name: "GetTestOversight",
		Description: "Test oversight summary for a task.",
		Tags:        []string{"tasks"},
	},

	// --- Admin operations ---

	{
		Method: http.MethodPost, Pattern: "/api/admin/rebuild-index", Name: "RebuildIndex",
		Description: "Rebuild the in-memory search index from disk; returns the number of repaired entries.",
		Tags:        []string{"admin"},
	},

	// --- Refinement agent ---

	{
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/refine", Name: "StartRefinement",
		JSName:      "refine",
		Description: "Start the refinement sandbox agent for a backlog task.",
		Tags:        []string{"tasks", "refine"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/tasks/{id}/refine", Name: "CancelRefinement",
		JSName:      "refine",
		Description: "Cancel an in-progress refinement agent.",
		Tags:        []string{"tasks", "refine"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/{id}/refine/logs", Name: "StreamRefineLogs",
		Description: "Stream live logs from the refinement agent.",
		Tags:        []string{"tasks", "refine", "sse"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/refine/apply", Name: "RefineApply",
		Description: "Apply the refined prompt as the new task prompt.",
		Tags:        []string{"tasks", "refine"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/refine/dismiss", Name: "RefineDismiss",
		Description: "Dismiss the refinement result without applying it.",
		Tags:        []string{"tasks", "refine"},
	},
}
