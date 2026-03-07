// --- Brainstorm / Ideation agent ---

// Client-side ideation state (mirrors server config).
let ideation = false;
let ideationInterval = 0;  // minutes; 0 = run immediately on completion
let ideationNextRun = null; // ISO timestamp string, or null

// setIdeationRunning shows/hides the header spinner.
function setIdeationRunning(running) {
  const spinner = document.getElementById('ideation-spinner');
  if (spinner) spinner.style.display = running ? 'inline-block' : 'none';
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

// updateNextRunDisplay refreshes the "next in Xm" label next to the selector.
function updateNextRunDisplay() {
  const el = document.getElementById('ideation-next-run');
  if (!el) return;

  // Only show when ideation is enabled, interval > 0, and a run is pending.
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
  let label;
  if (diffMin < 60) {
    label = `next in ${diffMin}m`;
  } else {
    const h = Math.floor(diffMin / 60);
    const m = diffMin % 60;
    label = m > 0 ? `next in ${h}h ${m}m` : `next in ${h}h`;
  }
  el.textContent = label;
  el.style.display = 'inline';
}

// _syncIdeationControls shows/hides the interval selector based on ideation state.
function _syncIdeationControls() {
  const sel = document.getElementById('ideation-interval');
  if (sel) sel.style.display = ideation ? 'inline-block' : 'none';
}

// updateIdeationConfig updates local state from a config response object.
// Called by fetchConfig after the initial load.
function updateIdeationConfig(cfg) {
  ideation = !!cfg.ideation;
  ideationInterval = cfg.ideation_interval != null ? cfg.ideation_interval : 0;
  ideationNextRun = cfg.ideation_next_run || null;

  const toggle = document.getElementById('ideation-toggle');
  if (toggle) toggle.checked = ideation;

  const sel = document.getElementById('ideation-interval');
  if (sel) {
    sel.value = String(ideationInterval);
    sel.style.display = ideation ? 'inline-block' : 'none';
  }

  updateNextRunDisplay();
}

// Refresh the countdown display every 30 seconds so it stays accurate.
setInterval(updateNextRunDisplay, 30000);
