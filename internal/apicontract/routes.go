// Package apicontract is the single source of truth for all HTTP API routes.
//
// Routes is the canonical list used to:
//   - Register handlers in the HTTP multiplexer (server.go buildMux).
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

	// --- Routines ---

	{
		Method: http.MethodGet, Pattern: "/api/routines", Name: "ListRoutines",
		JSName:      "list",
		Description: "List routine task cards with their schedules and next-run times.",
		Tags:        []string{"routines"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/routines", Name: "CreateRoutine",
		JSName:      "create",
		Description: "Create a new routine card that spawns instance tasks on a fixed interval.",
		Tags:        []string{"routines"},
	},
	{
		Method: http.MethodPatch, Pattern: "/api/routines/{id}/schedule", Name: "UpdateRoutineSchedule",
		JSName:      "updateSchedule",
		Description: "Update a routine's interval or enabled flag; unset fields are left unchanged.",
		Tags:        []string{"routines"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/routines/{id}/trigger", Name: "TriggerRoutine",
		JSName:      "trigger",
		Description: "Fire a routine immediately, bypassing the schedule; the scheduled cycle continues.",
		Tags:        []string{"routines"},
	},

	// --- Agents ---

	{
		Method: http.MethodGet, Pattern: "/api/agents", Name: "ListAgents",
		JSName:      "list",
		Description: "List all registered sub-agent roles (built-in catalog).",
		Tags:        []string{"agents"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/agents/{slug}", Name: "GetAgent",
		JSName:      "get",
		Description: "Get one agent's full descriptor including its prompt template body.",
		Tags:        []string{"agents"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/agents", Name: "CreateAgent",
		JSName:      "create",
		Description: "Create a user-authored agent (rejects slugs that shadow a built-in).",
		Tags:        []string{"agents"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/agents/{slug}", Name: "UpdateAgent",
		JSName:      "update",
		Description: "Update a user-authored agent; 409 for built-in slugs.",
		Tags:        []string{"agents"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/agents/{slug}", Name: "DeleteAgent",
		JSName:      "delete",
		Description: "Delete a user-authored agent; 409 for built-in slugs.",
		Tags:        []string{"agents"},
	},

	// --- Flows ---

	{
		Method: http.MethodGet, Pattern: "/api/flows", Name: "ListFlows",
		JSName:      "list",
		Description: "List all registered flows (built-in catalog).",
		Tags:        []string{"flows"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/flows/{slug}", Name: "GetFlow",
		JSName:      "get",
		Description: "Get one flow's full descriptor including its step chain and agent names.",
		Tags:        []string{"flows"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/flows", Name: "CreateFlow",
		JSName:      "create",
		Description: "Create a user-authored flow (rejects slugs that shadow a built-in).",
		Tags:        []string{"flows"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/flows/{slug}", Name: "UpdateFlow",
		JSName:      "update",
		Description: "Update a user-authored flow; 409 for built-in slugs.",
		Tags:        []string{"flows"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/flows/{slug}", Name: "DeleteFlow",
		JSName:      "delete",
		Description: "Delete a user-authored flow; 409 for built-in slugs.",
		Tags:        []string{"flows"},
	},

	// --- Spec tree ---

	{
		Method: http.MethodGet, Pattern: "/api/specs/tree", Name: "GetSpecTree",
		JSName:      "tree",
		Description: "Get the full spec tree with metadata and progress.",
		Tags:        []string{"specs"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/specs/stream", Name: "SpecTreeStream",
		Description: "SSE stream of spec tree change notifications.",
		Tags:        []string{"specs", "sse"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/specs/transition", Name: "SpecTransition",
		Description: "Spec lifecycle transition. Body {action, ...}: dispatch/undispatch take paths[] (and run for dispatch) and return per-spec arrays; archive/unarchive take a single path and return {path, status}.",
		Tags:        []string{"specs"},
	},

	// --- Spec comments (coordination plane) ---
	{
		Method: http.MethodGet, Pattern: "/api/spec-comments", Name: "ListSpecComments",
		JSName:      "listSpecComments",
		Description: "List cloud-resident spec comment threads for the visible repos, each repositioned against the current spec body (orphaned flag set when the anchor is lost).",
		Tags:        []string{"spec-comments"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/spec-comments", Name: "SubmitSpecComment",
		JSName:      "submitSpecComment",
		Description: "Forward a spec-comment op (create/reply/resolve/reopen) up the coordination connection. The coordinator is authoritative and echoes the result back over the SSE stream.",
		Tags:        []string{"spec-comments"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/spec-comments/stream", Name: "StreamSpecComments",
		JSName:      "specCommentsStream",
		Description: "SSE stream of spec-comment events relayed from the coordinator (create/reply/resolve/reopen/sync).",
		Tags:        []string{"spec-comments", "sse"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/coordination/status", Name: "GetCoordinationStatus",
		JSName:      "coordinationStatus",
		Description: "Report whether the coordination opt-in is enabled (and available).",
		Tags:        []string{"spec-comments"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/coordination/opt-in", Name: "SetCoordinationOptIn",
		JSName:      "setCoordinationOptIn",
		Description: "Flip the coordination opt-in (the data-boundary gate). Body {enabled}.",
		Tags:        []string{"spec-comments"},
	},

	// --- Planning sandbox ---

	{
		Method: http.MethodGet, Pattern: "/api/planning", Name: "GetPlanningStatus",
		JSName:      "status",
		Description: "Get planning sandbox status.",
		Tags:        []string{"planning"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/planning", Name: "StartPlanning",
		JSName:      "start",
		Description: "Start the planning sandbox.",
		Tags:        []string{"planning"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/planning", Name: "StopPlanning",
		JSName:      "stop",
		Description: "Stop the planning sandbox.",
		Tags:        []string{"planning"},
	},

	// --- Planning messages ---

	{
		Method: http.MethodGet, Pattern: "/api/planning/messages", Name: "GetPlanningMessages",
		JSName:      "messages",
		Description: "Retrieve conversation history.",
		Tags:        []string{"planning"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/planning/messages", Name: "SendPlanningMessage",
		JSName:      "sendMessage",
		Description: "Send a user message, triggers agent exec.",
		Tags:        []string{"planning"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/planning/messages", Name: "ClearPlanningMessages",
		JSName:      "clearMessages",
		Description: "Clear conversation history.",
		Tags:        []string{"planning"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/planning/messages/stream", Name: "StreamPlanningMessages",
		JSName:      "messageStream",
		Description: "Stream the agent's response tokens.",
		Tags:        []string{"planning"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/planning/messages/interrupt", Name: "InterruptPlanningMessage",
		JSName:      "interruptMessage",
		Description: "Interrupt the current agent turn.",
		Tags:        []string{"planning"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/planning/undo", Name: "UndoPlanningRound",
		JSName:      "undo",
		Description: "Undo the last planning round (git reset --hard on the last commit carrying the Plan-Round trailer).",
		Tags:        []string{"planning"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/planning/commands", Name: "GetPlanningCommands",
		JSName:      "commands",
		Description: "List available slash commands.",
		Tags:        []string{"planning"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/planning/tool/update_task_prompt", Name: "UpdateTaskPromptTool",
		JSName:      "updateTaskPromptTool",
		Description: "Tool endpoint: update a task's prompt from a task-mode planning thread.",
		Tags:        []string{"planning"},
	},

	// --- Planning chat threads ---

	{
		Method: http.MethodGet, Pattern: "/api/planning/threads", Name: "ListPlanningThreads",
		JSName:      "listThreads",
		Description: "List planning chat threads for the current workspace group.",
		Tags:        []string{"planning"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/planning/threads", Name: "CreatePlanningThread",
		JSName:      "createThread",
		Description: "Create a new planning chat thread.",
		Tags:        []string{"planning"},
	},
	{
		Method: http.MethodPatch, Pattern: "/api/planning/threads/{id}", Name: "PatchPlanningThread",
		JSName:      "patchThread",
		Description: "Mutate a planning chat thread: {name} renames; {state: archived|visible|active} archives, restores, or activates it.",
		Tags:        []string{"planning"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/planning/threads/{id}", Name: "DeletePlanningThread",
		JSName:      "deleteThread",
		Description: "Permanently delete an archived planning chat thread and its stored conversation.",
		Tags:        []string{"planning"},
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
		Description: "Update task fields: status (incl. status=cancelled, which kills the worker and cleans worktrees), prompt, timeout, sandbox, dependencies, fresh_start, archived (true/false), deleted=false (restore).",
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
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/resume", Name: "ResumeTask",
		Description: "Resume a failed or waiting task using its existing session.",
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
		Description: "Oversight summary for a task. ?phase=impl (default) or ?phase=test selects the implementation- or test-agent summary.",
		Tags:        []string{"tasks"},
	},

	// --- Admin operations ---

	{
		Method: http.MethodPost, Pattern: "/api/admin/rebuild-index", Name: "RebuildIndex",
		Description: "Rebuild the in-memory search index from disk; returns the number of repaired entries.",
		Tags:        []string{"admin"},
	},

	// --- File explorer ---

	{
		Method: http.MethodGet, Pattern: "/api/explorer/tree", Name: "ExplorerTree",
		JSName:      "tree",
		Description: "List one level of a workspace directory.",
		Tags:        []string{"explorer"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/explorer/stream", Name: "ExplorerStream",
		Description: "SSE stream of file tree change notifications for workspace directories.",
		Tags:        []string{"explorer", "sse"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/explorer/file", Name: "ExplorerReadFile",
		JSName:      "readFile",
		Description: "Read file contents from a workspace.",
		Tags:        []string{"explorer"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/explorer/file", Name: "ExplorerWriteFile",
		JSName:      "writeFile",
		Description: "Write file contents to a workspace.",
		Tags:        []string{"explorer"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/explorer/file/stream", Name: "ExplorerFileStream",
		Description: "SSE stream that notifies when a single watched file's contents change.",
		Tags:        []string{"explorer", "sse"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/explorer/task-prompts", Name: "ExplorerTaskPrompts",
		JSName:      "taskPrompts",
		Description: "List backlog (and optionally waiting) tasks as virtual entries for the workspace explorer Task Prompts section.",
		Tags:        []string{"explorer"},
	},

	// --- OAuth authentication ---

	{
		Method: http.MethodPost, Pattern: "/api/auth/{provider}/start", Name: "StartOAuth",
		Description: "Start an OAuth authorization flow for the given provider (claude or codex).",
		Tags:        []string{"auth"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/auth/{provider}/status", Name: "OAuthStatus",
		Description: "Poll the current status of an OAuth flow for the given provider.",
		Tags:        []string{"auth"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/auth/{provider}/cancel", Name: "CancelOAuth",
		Description: "Cancel an in-progress OAuth flow for the given provider.",
		Tags:        []string{"auth"},
	},

	// --- Latere.ai sign-in (cloud mode only; mounted when WALLFACER_CLOUD=true) ---

	{
		Method: http.MethodGet, Pattern: "/login", Name: "Login",
		Description: "Redirect to the latere.ai auth service to begin sign-in.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodGet, Pattern: "/callback", Name: "Callback",
		Description: "OAuth2 authorization-code callback; sets the session cookie.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodGet, Pattern: "/logout", Name: "Logout",
		Description: "Clear the local session and redirect to the auth service logout.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodGet, Pattern: "/logout/notify", Name: "LogoutNotify",
		Description: "Front-channel logout target: clear the local cookie when the user signs out centrally.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/me", Name: "AuthMe",
		JSName:      "authMe",
		Description: "Return the current signed-in user, or 204 when unauthenticated.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/auth/orgs", Name: "AuthOrgs",
		JSName:      "authOrgs",
		Description: "List the signed-in user's organizations; 204 when single-org or unauthenticated.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodPatch, Pattern: "/api/auth/me", Name: "PatchAuthMe",
		JSName:      "patchAuthMe",
		Description: "Mutate the signed-in principal — currently only org_id (active organization). Clears session and returns a redirect to /login?org_id=<target>.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/me/switch-org", Name: "SwitchOrg",
		JSName:      "switchOrg",
		Description: "Switch the active organization (latere-ui session convention). Validates membership, clears the session, and returns {redirect} to /login?org_id=<target>.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/auth/device/start", Name: "AuthDeviceStart",
		JSName:      "authDeviceStart",
		Description: "Local-mode RFC 8628 device-code: start a sign-in flow and return the user code + verification URI.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/auth/device/poll", Name: "AuthDevicePoll",
		JSName:      "authDevicePoll",
		Description: "Poll the in-flight local-mode device-code flow; returns {status: idle|pending|done|denied|expired}.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/auth/device/cancel", Name: "AuthDeviceCancel",
		JSName:      "authDeviceCancel",
		Description: "Cancel the in-flight local-mode device-code flow.",
		Tags:        []string{"login"},
	},
}
