// --- Brainstorm / Ideation agent ---

// Client-side ideation state (mirrors server config).
let ideation = false;
let ideationInterval = 0; // minutes; 0 = run immediately on completion
let ideationNextRun = null; // ISO timestamp string, or null
let _ideationRunning = false;
let ideationExploitRatio = 0.8; // 0.0–1.0; fraction of exploitation ideas

// setIdeationRunning tracks whether a brainstorm task is currently in progress
// and refreshes the header label accordingly.
function setIdeationRunning(running) {
  _ideationRunning = running;
  updateNextRunDisplay();
}

// updateIdeationFromTasks derives the running state from the live task list
// (via SSE) instead of polling. Called whenever the task list is refreshed.
function updateIdeationFromTasks(tasks) {
  const running = tasks.some(
    (t) => t.kind === "idea-agent" && t.status === "in_progress",
  );
  setIdeationRunning(running);
}

// toggleIdeation is called by the brainstorm checkbox in the header.
async function toggleIdeation() {
  const toggle = document.getElementById("ideation-toggle");
  const enabled = toggle ? toggle.checked : !ideation;
  try {
    const res = await api("/api/config", {
      method: "PUT",
      body: JSON.stringify({ ideation: enabled }),
    });
    ideation = !!res.ideation;
    ideationNextRun = res.ideation_next_run || null;
    if (toggle) toggle.checked = ideation;
    _syncIdeationControls();
    updateNextRunDisplay();
  } catch (e) {
    showAlert("Error toggling brainstorm: " + e.message);
    if (toggle) toggle.checked = ideation;
  }
}

// triggerIdeation creates an idea-agent task card immediately via POST /api/ideate.
async function triggerIdeation() {
  try {
    const res = await api("/api/ideate", { method: "POST" });
    if (res && res.task_id) {
      waitForTaskDelta(res.task_id);
    } else {
      fetchTasks();
    }
  } catch (e) {
    showAlert("Error triggering brainstorm: " + e.message);
  }
}

// updateIdeationInterval is called when the interval selector changes.
async function updateIdeationInterval(minutes) {
  try {
    const res = await api("/api/config", {
      method: "PUT",
      body: JSON.stringify({ ideation_interval: parseInt(minutes, 10) }),
    });
    ideationInterval =
      res.ideation_interval != null ? res.ideation_interval : 0;
    ideationNextRun = res.ideation_next_run || null;
    const sel = document.getElementById("ideation-interval");
    if (sel) sel.value = String(ideationInterval);
    updateNextRunDisplay();
  } catch (e) {
    showAlert("Error updating brainstorm interval: " + e.message);
  }
}

// updateNextRunDisplay refreshes the header label that shows when the next
// brainstorm run is scheduled, or indicates that one is currently running.
function updateNextRunDisplay() {
  const el = document.getElementById("ideation-next-run");
  if (!el) return;

  if (_ideationRunning) {
    el.textContent = "Brainstorm running\u2026";
    el.style.display = "inline";
    return;
  }

  // Only show countdown when ideation is enabled, interval > 0, and a run is pending.
  if (!ideation || ideationInterval === 0 || !ideationNextRun) {
    el.textContent = "";
    el.style.display = "none";
    return;
  }

  const nextRun = new Date(ideationNextRun);
  if (Number.isNaN(nextRun.getTime())) {
    el.style.display = "none";
    return;
  }

  const diffMs = nextRun - Date.now();
  if (diffMs <= 0) {
    el.textContent = "";
    el.style.display = "none";
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
  el.style.display = "inline";
}

// updateExploitRatioLabel updates the label text while the slider is dragged (oninput).
function updateExploitRatioLabel(pct) {
  const label = document.getElementById("ideation-exploit-ratio-label");
  if (label) label.textContent = `${pct}/${100 - parseInt(pct, 10)}`;
}

// updateIdeationExploitRatio persists the slider value via PUT /api/config.
async function updateIdeationExploitRatio(pct) {
  const ratio = parseInt(pct, 10) / 100;
  try {
    const res = await api("/api/config", {
      method: "PUT",
      body: JSON.stringify({ ideation_exploit_ratio: ratio }),
    });
    ideationExploitRatio =
      res.ideation_exploit_ratio != null ? res.ideation_exploit_ratio : 0.8;
    _syncExploitRatioSlider();
  } catch (e) {
    showAlert("Error updating exploit ratio: " + e.message);
  }
}

// _syncExploitRatioSlider updates the slider and label to match state.
function _syncExploitRatioSlider() {
  const pct = Math.round(ideationExploitRatio * 100);
  const slider = document.getElementById("ideation-exploit-ratio");
  if (slider) slider.value = String(pct);
  const label = document.getElementById("ideation-exploit-ratio-label");
  if (label) label.textContent = `${pct}/${100 - pct}`;
}

// _syncIdeationControls keeps the settings modal and header controls in sync with state.
function _syncIdeationControls() {
  const toggle = document.getElementById("ideation-toggle");
  if (toggle) toggle.checked = ideation;
  const headerToggle = document.getElementById("ideation-header-toggle");
  if (headerToggle) headerToggle.checked = ideation;
  const sel = document.getElementById("ideation-interval");
  if (sel) sel.value = String(ideationInterval);
  _syncExploitRatioSlider();
}

// toggleIdeationHeader is called by the header toggle chip.
// It delegates to the existing toggleIdeation logic.
async function toggleIdeationHeader() {
  const headerToggle = document.getElementById("ideation-header-toggle");
  const enabled = headerToggle ? headerToggle.checked : !ideation;
  try {
    const res = await api("/api/config", {
      method: "PUT",
      body: JSON.stringify({ ideation: enabled }),
    });
    ideation = !!res.ideation;
    ideationNextRun = res.ideation_next_run || null;
    _syncIdeationControls();
    updateNextRunDisplay();
    if (typeof updateAutomationActiveCount === "function")
      updateAutomationActiveCount();
  } catch (e) {
    showAlert("Error toggling brainstorm: " + e.message);
    if (headerToggle) headerToggle.checked = ideation;
  }
}

// updateIdeationConfig updates local state from a config response object.
// Called by fetchConfig after the initial load.
function updateIdeationConfig(cfg) {
  ideation = !!cfg.ideation;
  ideationInterval = cfg.ideation_interval != null ? cfg.ideation_interval : 0;
  ideationNextRun = cfg.ideation_next_run || null;
  ideationExploitRatio =
    cfg.ideation_exploit_ratio != null ? cfg.ideation_exploit_ratio : 0.8;

  _syncIdeationControls();
  updateNextRunDisplay();
}

// Refresh the countdown display every 30 seconds so it stays accurate.
setInterval(updateNextRunDisplay, 30000);
