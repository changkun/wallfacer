// --- Global state ---
let tasks = [];
let currentTaskId = null;
let logsAbort = null;
let rawLogBuffer = '';
// logsMode: 'pretty' | 'raw' | 'oversight'
let logsMode = 'pretty';

// Test agent monitor state (shown alongside impl logs when is_test_run=true)
let testLogsAbort = null;
let testRawLogBuffer = '';
// testLogsMode: 'pretty' | 'raw' | 'oversight'
let testLogsMode = 'pretty';
let showArchived = localStorage.getItem('wallfacer-show-archived') === 'true';

// Tasks SSE state
let tasksSource = null;
let tasksRetryDelay = 1000;

// Git SSE state
let gitStatuses = [];
let gitStatusSource = null;
let gitRetryDelay = 1000;

// Autopilot state
let autopilot = false;

// Max parallel tasks (loaded from /api/env, 0 = not yet loaded)
let maxParallelTasks = 0;

// Refine logs state
let refineRawLogBuffer = '';
// refineLogsMode: 'pretty' | 'raw'
let refineLogsMode = 'pretty';

// Debounce timer for backlog prompt auto-save
let editDebounce = null;
