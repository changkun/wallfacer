// --- Global state ---
let tasks = [];
let currentTaskId = null;
let logsAbort = null;
let showArchived = localStorage.getItem('wallfacer-show-archived') === 'true';

// Tasks SSE state
let tasksSource = null;
let tasksRetryDelay = 1000;

// Git SSE state
let gitStatuses = [];
let gitStatusSource = null;
let gitRetryDelay = 1000;

// Debounce timer for backlog prompt auto-save
let editDebounce = null;
