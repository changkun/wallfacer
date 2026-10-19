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
		Description: "Board manifest as it appeared to a specific task (is_self=true for that task).",
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
		Description: "Get server configuration (workspaces, workspace groups, sandbox list).",
		Tags:        []string{"config"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/config", Name: "UpdateConfig",
		Description: "Update server configuration (workspace groups and their parallel limits).",
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
		Description: "List workspaces, each flagged active or dormant (a dormant one waits to be re-pointed to one folder).",
		Tags:        []string{"workspaces"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/workspaces", Name: "CreateWorkspace",
		Description: "Create a workspace (stable id, random storage key) from a name and one folder; 400 when more than one folder is given.",
		Tags:        []string{"workspaces"},
	},
	{
		Method: http.MethodPut, Pattern: "/api/workspaces/{id}", Name: "UpdateWorkspace",
		Description: "Rename a workspace and/or re-point it to one folder without re-keying its history; re-pointing clears dormant. 400 when more than one folder is given.",
		Tags:        []string{"workspaces"},
	},
	{
		Method: http.MethodDelete, Pattern: "/api/workspaces/{id}", Name: "DeleteWorkspace",
		Description: "Delete a workspace and wipe its task history and session data; folders on disk are not touched. Returns the resulting config.",
		Tags:        []string{"workspaces"},
	},
	{
		Method: http.MethodPost, Pattern: "/api/workspaces/{id}/activate", Name: "ActivateWorkspace",
		Description: "Activate a workspace by id and switch the scoped task board; 400 for a dormant workspace that still names several folders.",
		Tags:        []string{"workspaces"},
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
		Method: http.MethodGet, Pattern: "/api/tasks/{id}/commits", Name: "TaskCommits",
		Description: "Read preserved task commits and patches across feedback turns and retries.",
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
}
