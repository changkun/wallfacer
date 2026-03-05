// --- Global state ---
let tasks = [];
let currentTaskId = null;
let logsAbort = null;
let rawLogBuffer = '';
let logsPrettyMode = true;
let showArchived = localStorage.getItem('wallfacer-show-archived') === 'true';

// Tasks SSE state
let tasksSource = null;
let tasksRetryDelay = 1000;

// Git SSE state
let gitStatuses = [];
let gitStatusSource = null;
let gitRetryDelay = 1000;

// Autopilot state
let autopilot = true;

// Debounce timer for backlog prompt auto-save
let editDebounce = null;
