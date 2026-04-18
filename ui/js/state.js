// --- Global state ---
let tasks = [];
// Set of task IDs for which a cancel request has been sent but the SSE update
// confirming the status change has not yet arrived. Used to show a "cancelling"
// indicator on board cards while the container is shutting down.
let pendingCancelTaskIds = new Set();
let logsAbort = null;
let rawLogBuffer = "";
// logsMode: 'pretty' | 'raw' | 'oversight'
let logsMode = "pretty";
// logSearchQuery: active filter string for the implementation log viewer
let logSearchQuery = "";

// Test agent monitor state (shown alongside impl logs when is_test_run=true)
let testLogsAbort = null;
let testRawLogBuffer = "";
// testLogsMode: 'pretty' | 'raw' | 'oversight'
let testLogsMode = "pretty";
let showArchived = localStorage.getItem("wallfacer-show-archived") === "true";
let archivedTasks = [];
let archivedTasksPageSize = 20;
var archivedPage = {
  // Invariant: at most one direction loads at a time.
  // 'idle' | 'loading-before' | 'loading-after'
  loadState: "idle",
  hasMoreBefore: false,
  hasMoreAfter: false,
};
let archivedScrollHandlerBound = false;

// Tasks SSE state
let tasksSource = null;
let tasksRetryDelay = 1000;
// Effective SSE connection state for the status bar. Set by both leader
// (from EventSource.readyState) and follower (from BroadcastChannel activity).
// Values: "ok" | "reconnecting" | "closed"
let _sseConnState = "closed";
// lastTasksEventId holds the SSE id: value from the most recently received
// task stream event. Passed as ?last_event_id=<id> on reconnect to enable
// delta replay instead of a full snapshot.
let lastTasksEventId = null;

// Git SSE state
let gitStatuses = [];
let gitStatusSource = null;
let gitRetryDelay = 1000;
let activeWorkspaces = [];
let workspaceGroups = [];
let workspacePickerRequired = false;
let workspaceBrowserPath = "";
let workspaceBrowserEntries = [];
let workspaceBrowserFocusIndex = -1;
let workspaceBrowserFilterQuery = "";
let workspaceSelectionDraft = [];
let workspaceGroupSwitchingIndex = -1;
let workspaceGroupSwitching = false;
let hiddenGroupIndices = new Set();
// Per-group task counts from the config API: [{key, in_progress, waiting}, ...]
let activeGroups = [];

// Automation toggle state
let autopilot = false;
let autorefine = false;
let autotest = false;
let autosubmit = false;
let autosync = false;
let autopush = false;

// Terminal feature gate (set by fetchConfig from server)
let terminalEnabled = false;

// Host execution mode flag (set by fetchConfig from server). True when
// the runner is configured with the HostBackend — no container, the
// agent CLI runs directly as a host process. UI surfaces that only
// apply to containerised execution read this to hide themselves.
let hostMode = false;

// Max parallel tasks (loaded from /api/env, 0 = not yet loaded)
let maxParallelTasks = 0;

// Refine logs state
let refineRawLogBuffer = "";
// refineLogsMode: 'pretty' | 'raw'
let refineLogsMode = "pretty";

// Debounce timer for backlog prompt auto-save
let editDebounce = null;

// Timeline auto-refresh timer (setInterval ID or null)
let timelineRefreshTimer = null;

// Search / filter state
let filterQuery = "";
let backlogSortMode =
  localStorage.getItem("wallfacer-backlog-sort-mode") === "impact"
    ? "impact"
    : "manual";

// Task change listeners — lightweight observer for modules that need to react
// to task list changes without coupling to the SSE handlers.
let _taskChangeListeners = [];
function registerTaskChangeListener(fn) {
  _taskChangeListeners.push(fn);
}
function notifyTaskChangeListeners() {
  for (var i = 0; i < _taskChangeListeners.length; i++) {
    try {
      _taskChangeListeners[i](tasks);
    } catch (e) {
      console.error("task change listener error:", e);
    }
  }
}

// Deep-link hash handling: true once the initial URL hash has been processed.
let _hashHandled = false;
