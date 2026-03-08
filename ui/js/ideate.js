// --- Brainstorm / Ideation agent ---

// Client-side ideation state (mirrors server config).
let ideation = false;
let ideationInterval = 0;  // minutes; 0 = run immediately on completion
let ideationNextRun = null; // ISO timestamp string, or null
let _ideationRunning = false;

// setIdeationRunning tracks whether a brainstorm task is currently in progress
// and refreshes the header label accordingly.
function setIdeationRunning(running) {
  _ideationRunning = running;
  updateNextRunDisplay();
}

// updateIdeationFromTasks derives the running state from the live task list
// (via SSE) instead of polling. Called whenever the task list is refreshed.
function updateIdeationFromTasks(tasks) {
  const running = tasks.some(t => t.kind === 'idea-agent' && t.status === 'in_progress');
  setIdeationRunning(running);
}

// toggleIdeation is called by the brainstorm checkbox in the header.
async function toggleIdeation() {
  const toggle = document.getElementById('ideation-toggle');
  const enabled = toggle ? toggle.checked : !ideation;
  try {
    const res = await api('/api/config', {
      method: 'PUT',
      body: JSON.stringify({ ideation: enabled }),
    });
    ideation = !!res.ideation;
    ideationNextRun = res.ideation_next_run || null;
    if (toggle) toggle.checked = ideation;
    _syncIdeationControls();
    updateNextRunDisplay();
  } catch (e) {
    showAlert('Error toggling brainstorm: ' + e.message);
    if (toggle) toggle.checked = ideation;
  }
}

// triggerIdeation creates an idea-agent task card immediately via POST /api/ideate.
async function triggerIdeation() {
  try {
    await api('/api/ideate', { method: 'POST' });
  } catch (e) {
    showAlert('Error triggering brainstorm: ' + e.message);
  }
}

// updateIdeationInterval is called when the interval selector changes.
async function updateIdeationInterval(minutes) {
  try {
    const res = await api('/api/config', {
      method: 'PUT',
      body: JSON.stringify({ ideation_interval: parseInt(minutes, 10) }),
    });
    ideationInterval = res.ideation_interval != null ? res.ideation_interval : 0;
    ideationNextRun = res.ideation_next_run || null;
    const sel = document.getElementById('ideation-interval');
    if (sel) sel.value = String(ideationInterval);
    updateNextRunDisplay();
  } catch (e) {
    showAlert('Error updating brainstorm interval: ' + e.message);
  }
}

// updateNextRunDisplay refreshes the header label that shows when the next
// brainstorm run is scheduled, or indicates that one is currently running.
function updateNextRunDisplay() {
  const el = document.getElementById('ideation-next-run');
  if (!el) return;

  if (_ideationRunning) {
    el.textContent = 'Brainstorm running\u2026';
    el.style.display = 'inline';
    return;
  }

  // Only show countdown when ideation is enabled, interval > 0, and a run is pending.
  if (!ideation || ideationInterval === 0 || !ideationNextRun) {
    el.textContent = '';
    el.style.display = 'none';
    return;
  }

  const nextRun = new Date(ideationNextRun);
  if (isNaN(nextRun.getTime())) {
    el.style.display = 'none';
    return;
  }

  const diffMs = nextRun - Date.now();
  if (diffMs <= 0) {
    el.textContent = '';
    el.style.display = 'none';
    return;
  }

  const diffMin = Math.ceil(diffMs / 60000);
  let countdown;
  if (diffMin < 60) {
    countdown = `${diffMin}m`;
  } else {
    const h = Math.floor(diffMin / 60);
    const m = diffMin % 60;
    countdown = m > 0 ? `${h}h ${m}m` : `${h}h`;
  }
  el.textContent = `Next brainstorm in ${countdown}`;
  el.style.display = 'inline';
}

// _syncIdeationControls keeps the settings modal controls in sync with state.
function _syncIdeationControls() {
  const toggle = document.getElementById('ideation-toggle');
  if (toggle) toggle.checked = ideation;
  const sel = document.getElementById('ideation-interval');
  if (sel) sel.value = String(ideationInterval);
}

// updateIdeationConfig updates local state from a config response object.
// Called by fetchConfig after the initial load.
function updateIdeationConfig(cfg) {
  ideation = !!cfg.ideation;
  ideationInterval = cfg.ideation_interval != null ? cfg.ideation_interval : 0;
  ideationNextRun = cfg.ideation_next_run || null;

  _syncIdeationControls();
  updateNextRunDisplay();
}

// Refresh the countdown display every 30 seconds so it stays accurate.
setInterval(updateNextRunDisplay, 30000);
