// --- Global state ---
let tasks = [];
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

// Automation toggle state
let autopilot = false;
let autorefine = false;
let autotest = false;
let autosubmit = false;
let autosync = false;
let autopush = false;

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

// Deep-link hash handling: true once the initial URL hash has been processed.
let _hashHandled = false;
