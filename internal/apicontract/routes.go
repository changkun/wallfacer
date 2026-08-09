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
		Description: "File listing for @ mention autocomplete.",
		Tags:        []string{"files"},
	},

	// --- Server configuration ---

	{
		Method: http.MethodGet, Pattern: "/api/config", Name: "GetConfig",
		Description: "Get server configuration (workspaces, autoimplement flags, sandbox list).",
		Tags:        []string{"config"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/config", Name: "UpdateConfig",
		Description: "Update server configuration (autoimplement, autotest, autosubmit, sandbox assignments).",
		Tags:        []string{"config"},
	},

	// --- Workspace selection ---

	{
		Method: http.MethodGet, Pattern: "/api/workspaces/browse", Name: "BrowseWorkspaces",
		Description: "List child directories for an absolute host path.",
		Tags:        []string{"workspaces"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/workspaces/pick-folder", Name: "PickFolder",
		Description: "Open the host OS native folder chooser and return the picked absolute path. Local-display only; 501 when no native picker is available (headless/cloud).",
		Tags:        []string{"workspaces"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/workspaces/mkdir", Name: "MkdirWorkspace",
		Description: "Create a new directory under an absolute host path.",
		Tags:        []string{"workspaces"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/workspaces/rename", Name: "RenameWorkspace",
		Description: "Rename a directory at an absolute host path.",
		Tags:        []string{"workspaces"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/workspaces", Name: "ListWorkspaces",
		Description: "List workspaces visible to the caller, each flagged active or dormant.",
		Tags:        []string{"workspaces"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/workspaces", Name: "CreateWorkspace",
		Description: "Create a workspace (stable id, random storage key) from a name and folder set.",
		Tags:        []string{"workspaces"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/workspaces/{id}", Name: "UpdateWorkspace",
		Description: "Rename a workspace and/or replace its folder set without re-keying its history.",
		Tags:        []string{"workspaces"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/workspaces/{id}", Name: "DeleteWorkspace",
		Description: "Delete a workspace record (its data directory is left on disk).",
		Tags:        []string{"workspaces"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/workspaces/{id}/activate", Name: "ActivateWorkspace",
		Description: "Activate a workspace by id and switch the scoped task board.",
		Tags:        []string{"workspaces"},
	},

	// --- Routines ---

	{
		Method: http.MethodGet, Pattern: "/api/routines", Name: "ListRoutines",
		Description: "List routine task cards with their schedules and next-run times.",
		Tags:        []string{"routines"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/routines", Name: "CreateRoutine",
		Description: "Create a new routine card that spawns instance tasks on a fixed interval.",
		Tags:        []string{"routines"},
	},
	{
		Method: http.MethodPatch, Pattern: "/api/routines/{id}/schedule", Name: "UpdateRoutineSchedule",
		Description: "Update a routine's interval or enabled flag; unset fields are left unchanged.",
		Tags:        []string{"routines"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/routines/{id}/trigger", Name: "TriggerRoutine",
		Description: "Fire a routine immediately, bypassing the schedule; the scheduled cycle continues.",
		Tags:        []string{"routines"},
	},

	// --- Agents ---

	{
		Method: http.MethodGet, Pattern: "/api/agents", Name: "ListAgents",
		Description: "List all registered sub-agent roles (built-in catalog).",
		Tags:        []string{"agents"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/agents/{slug}", Name: "GetAgent",
		Description: "Get one agent's full descriptor including its prompt template body.",
		Tags:        []string{"agents"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/agents", Name: "CreateAgent",
		Description: "Create a user-authored agent (rejects slugs that shadow a built-in).",
		Tags:        []string{"agents"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/agents/{slug}", Name: "UpdateAgent",
		Description: "Update a user-authored agent; 409 for built-in slugs.",
		Tags:        []string{"agents"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/agents/{slug}", Name: "DeleteAgent",
		Description: "Delete a user-authored agent; 409 for built-in slugs.",
		Tags:        []string{"agents"},
	},

	// --- Flows ---

	{
		Method: http.MethodGet, Pattern: "/api/flows", Name: "ListFlows",
		Description: "List all registered flows (built-in catalog).",
		Tags:        []string{"flows"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/flows/{slug}", Name: "GetFlow",
		Description: "Get one flow's full descriptor including its step chain and agent names.",
		Tags:        []string{"flows"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/flows", Name: "CreateFlow",
		Description: "Create a user-authored flow (rejects slugs that shadow a built-in).",
		Tags:        []string{"flows"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/flows/{slug}", Name: "UpdateFlow",
		Description: "Update a user-authored flow; 409 for built-in slugs.",
		Tags:        []string{"flows"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/flows/{slug}", Name: "DeleteFlow",
		Description: "Delete a user-authored flow; 409 for built-in slugs.",
		Tags:        []string{"flows"},
	},

	// --- Spec tree ---

	{
		Method: http.MethodGet, Pattern: "/api/specs/tree", Name: "GetSpecTree",
		Description: "Get the full spec tree with metadata and progress.",
		Tags:        []string{"specs"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/specs/stream", Name: "SpecTreeStream",
		Description: "SSE stream of spec tree change notifications.",
		Tags:        []string{"specs", "sse"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/graph", Name: "GetGraph",
		Description: "Get the unified spec+task dependency graph (nodes, typed edges, critical path, blocked set) for the Map.",
		Tags:        []string{"specs", "tasks"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/specs/stale-candidates", Name: "StaleCandidates",
		Description: "Advisory scan: complete specs whose affects files changed since the spec's updated date. No mutation; returns {candidates:[{path,files,reason}]}.",
		Tags:        []string{"specs"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/specs/dismiss-stale-candidates", Name: "DismissAllStaleCandidates",
		Description: "Bulk-dismiss every stale candidate by bumping its updated timestamp (status unchanged), one commit per workspace. Returns {dismissed:N}.",
		Tags:        []string{"specs"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/specs/transition", Name: "SpecTransition",
		Description: "Spec lifecycle transition. Body {action, ...}: dispatch/undispatch take paths[] (and run for dispatch) and return per-spec arrays; archive/unarchive/validate/stale/dismiss-stale/force-complete take a single path and return {path, status}.",
		Tags:        []string{"specs"},
	},

	// --- Spec comments (coordination plane) ---
	{
		Method: http.MethodGet, Pattern: "/api/spec-comments", Name: "ListSpecComments",
		Description: "List cloud-resident spec comment threads for the visible repos, each repositioned against the current spec body (orphaned flag set when the anchor is lost).",
		Tags:        []string{"spec-comments"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/spec-comments", Name: "SubmitSpecComment",
		Description: "Forward a spec-comment op (create/reply/resolve/reopen) up the coordination connection. The coordinator is authoritative and echoes the result back over the SSE stream.",
		Tags:        []string{"spec-comments"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/spec-comments/stream", Name: "StreamSpecComments",
		Description: "SSE stream of spec-comment events relayed from the coordinator (create/reply/resolve/reopen/sync).",
		Tags:        []string{"spec-comments", "sse"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/coordination/status", Name: "GetCoordinationStatus",
		Description: "Report whether the coordination opt-in is enabled (and available).",
		Tags:        []string{"spec-comments"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/coordination/opt-in", Name: "SetCoordinationOptIn",
		Description: "Flip the coordination opt-in (the data-boundary gate). Body {enabled}.",
		Tags:        []string{"spec-comments"},
	},

	// --- Agent session ---

	{
		Method: http.MethodGet, Pattern: "/api/agent", Name: "GetAgentSessionStatus",
		Description: "Get agent session status.",
		Tags:        []string{"agent"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/agent", Name: "StartAgentSession",
		Description: "Start the agent session.",
		Tags:        []string{"agent"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/agent", Name: "StopAgentSession",
		Description: "Stop the agent session.",
		Tags:        []string{"agent"},
	},

	// --- Agent messages ---

	{
		Method: http.MethodGet, Pattern: "/api/agent/messages", Name: "GetAgentMessages",
		Description: "Retrieve conversation history.",
		Tags:        []string{"agent"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/agent/messages", Name: "SendAgentMessage",
		Description: "Send a user message, triggers agent exec.",
		Tags:        []string{"agent"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/agent/messages", Name: "ClearAgentMessages",
		Description: "Clear conversation history.",
		Tags:        []string{"agent"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/agent/messages/stream", Name: "StreamAgentMessages",
		Description: "Stream the agent's response tokens.",
		Tags:        []string{"agent"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/agent/messages/interrupt", Name: "InterruptAgentMessage",
		Description: "Interrupt the current agent turn.",
		Tags:        []string{"agent"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/agent/undo", Name: "UndoPlanningRound",
		Description: "Undo the last agent-session round (git reset --hard on the last commit carrying the Plan-Round trailer).",
		Tags:        []string{"agent"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/agent/commands", Name: "GetAgentCommands",
		Description: "List available slash commands.",
		Tags:        []string{"agent"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/agent/tool/update_task_prompt", Name: "UpdateTaskPromptTool",
		Description: "Tool endpoint: update a task's prompt from a task-mode agent session.",
		Tags:        []string{"agent"},
	},

	// --- Agent sessions ---

	{
		Method: http.MethodGet, Pattern: "/api/agent/sessions", Name: "ListAgentSessions",
		Description: "List agent sessions for the current workspace group.",
		Tags:        []string{"agent"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/agent/sessions", Name: "CreateAgentSession",
		Description: "Create a new agent session.",
		Tags:        []string{"agent"},
	},
	{
		Method: http.MethodPatch, Pattern: "/api/agent/sessions/{id}", Name: "PatchAgentSession",
		Description: "Mutate an agent session: {name} renames; {state: archived|visible|active} archives, restores, or activates it.",
		Tags:        []string{"agent"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/agent/sessions/{id}", Name: "DeleteAgentSession",
		Description: "Permanently delete an archived agent session and its stored conversation.",
		Tags:        []string{"agent"},
	},

	// --- Environment configuration ---

	{
		Method: http.MethodGet, Pattern: "/api/env", Name: "GetEnvConfig",
		Description: "Get environment configuration (tokens masked).",
		Tags:        []string{"env"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/env", Name: "UpdateEnvConfig",
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
		Description: "List all built-in system prompt templates with override status and content.",
		Tags:        []string{"system-prompts"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/system-prompts/{name}", Name: "GetSystemPrompt",
		Description: "Get a single built-in system prompt template by name.",
		Tags:        []string{"system-prompts"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/system-prompts/{name}", Name: "UpdateSystemPrompt",
		Description: "Write a user override for a built-in system prompt template; validates the template before writing.",
		Tags:        []string{"system-prompts"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/system-prompts/{name}", Name: "DeleteSystemPrompt",
		Description: "Remove the user override for a built-in system prompt template, restoring the embedded default.",
		Tags:        []string{"system-prompts"},
	},

	// --- Whiteboard ---

	{
		Method: http.MethodGet, Pattern: "/api/whiteboard", Name: "GetWhiteboard",
		Description: "Load the active workspace's whiteboard scene JSON (empty body when none saved yet).",
		Tags:        []string{"whiteboard"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/whiteboard", Name: "PutWhiteboard",
		Description: "Save the active workspace's whiteboard scene JSON.",
		Tags:        []string{"whiteboard"},
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
		Description: "Aggregated token and cost usage statistics.",
		Tags:        []string{"stats"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/stats", Name: "GetStats",
		Description: "Task status and workspace cost statistics. Optional ?workspace=<repo-root-path> restricts aggregation to tasks for that workspace (400 if no tasks match).",
		Tags:        []string{"stats"},
	},

	// --- Task collection (no {id}) ---

	{
		Method: http.MethodGet, Pattern: "/api/tasks", Name: "ListTasks",
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
		Description: "Create a new task in the backlog.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/batch", Name: "BatchCreateTasks",
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
		Description: "List immutable task summaries for completed tasks (cost dashboard, no full task.json read).",
		Tags:        []string{"tasks", "stats"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/deleted", Name: "ListDeletedTasks",
		Description: "List soft-deleted (tombstoned) tasks that are within the retention window.",
		Tags:        []string{"tasks"},
	},

	// --- Task instance operations (require {id}) ---

	{
		Method: http.MethodPatch, Pattern: "/api/tasks/{id}", Name: "UpdateTask",
		Description: "Update task fields: status (incl. status=cancelled, which kills the worker and cleans worktrees), prompt, timeout, sandbox, dependencies, fresh_start, archived (true/false), deleted=false (restore).",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/tasks/{id}", Name: "DeleteTask",
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
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/review", Name: "ReviewTask",
		Description: "Trigger adversarial review verification for a task.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/{id}/review/transcript", Name: "ReviewTranscript",
		Description: "Read the review verification trajectory (per-fork, per-round transcripts).",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/{id}/trace", Name: "TaskTrace",
		Description: "Read the agent-graph trace (nodes + edges) of an agentic-flow run.",
		Tags:        []string{"tasks"},
	},

	{
		Method: http.MethodGet, Pattern: "/api/tasks/{id}/diff", Name: "TaskDiff",
		Description: "Git diff of task worktrees versus the default branch.",
		Tags:        []string{"tasks"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/tasks/{id}/pr", Name: "TaskPRStatus",
		Description: "The GitHub pull request for the task's branch, or null.",
		Tags:        []string{"tasks", "github"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/pr", Name: "CreateTaskPR",
		Description: "Create (or return the existing) GitHub pull request for the task's branch; repo and base derived from the workspace.",
		Tags:        []string{"tasks", "github"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/tasks/{id}/pr/comment", Name: "TaskPRComment",
		Description: "Post a comment to the task's pull request.",
		Tags:        []string{"tasks", "github"},
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
		Description: "Read file contents from a workspace.",
		Tags:        []string{"explorer"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/explorer/file", Name: "ExplorerWriteFile",
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
		Description: "Return the current signed-in user, or 204 when unauthenticated.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/auth/orgs", Name: "AuthOrgs",
		Description: "List the signed-in user's organizations; 204 when single-org or unauthenticated.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodPatch, Pattern: "/api/auth/me", Name: "PatchAuthMe",
		Description: "Mutate the signed-in principal — currently only org_id (active organization). Clears session and returns a redirect to /login?org_id=<target>.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/me/switch-org", Name: "SwitchOrg",
		Description: "Switch the active organization (latere-ui session convention). Validates membership, clears the session, and returns {redirect} to /login?org_id=<target>.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/auth/device/start", Name: "AuthDeviceStart",
		Description: "Local-mode RFC 8628 device-code: start a sign-in flow and return the user code + verification URI.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/auth/device/poll", Name: "AuthDevicePoll",
		Description: "Poll the in-flight local-mode device-code flow; returns {status: idle|pending|done|denied|expired}.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/auth/device/cancel", Name: "AuthDeviceCancel",
		Description: "Cancel the in-flight local-mode device-code flow.",
		Tags:        []string{"login"},
	},
	{
		Method: http.MethodGet, Pattern: "/api/github/auth/status", Name: "GitHubAuthStatus",
		Description: "GitHub connection state for the principal: connected, login, account, granted permissions, and whether the connect flow is available.",
		Tags:        []string{"github"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/github/auth/connect", Name: "GitHubAuthConnect",
		Description: "Start the brokered \"Latere AI\" GitHub App install + grant flow. Gated on the ../auth broker.",
		Tags:        []string{"github"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/github/auth/disconnect", Name: "GitHubAuthDisconnect",
		Description: "Disconnect GitHub by clearing the principal's stored token.",
		Tags:        []string{"github"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/github/pulls", Name: "GitHubCreatePull",
		Description: "Create a pull request from head into base; returns the open PR if one already exists for the branch.",
		Tags:        []string{"github"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/github/comments", Name: "GitHubCreateComment",
		Description: "Post a conversation comment to a pull request or issue.",
		Tags:        []string{"github"},
	},
}
