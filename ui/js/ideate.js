// --- Brainstorm / Ideation agent ---

// Client-side ideation state (mirrors server state).
let ideation = false;
let ideationRunning = false;

// pollIdeationTimer is used to poll config while ideation is running.
let pollIdeationTimer = null;

// setIdeationRunning updates the spinner and running state.
function setIdeationRunning(running) {
  ideationRunning = running;
  const spinner = document.getElementById('ideation-spinner');
  if (spinner) spinner.style.display = running ? 'inline-block' : 'none';
}

// pollIdeationStatus periodically checks /api/config until ideation stops.
function startIdeationPoll() {
  if (pollIdeationTimer) return;
  pollIdeationTimer = setInterval(async function() {
    try {
      const cfg = await api('/api/config');
      setIdeationRunning(!!cfg.ideation_running);
      if (!cfg.ideation_running) {
        clearInterval(pollIdeationTimer);
        pollIdeationTimer = null;
      }
    } catch (e) {
      // ignore transient errors
    }
  }, 3000);
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
    if (toggle) toggle.checked = ideation;
    if (res.ideation_running) {
      setIdeationRunning(true);
      startIdeationPoll();
    }
  } catch (e) {
    showAlert('Error toggling brainstorm: ' + e.message);
    if (toggle) toggle.checked = ideation;
  }
}

// triggerIdeation manually fires one brainstorm run via POST /api/ideate.
async function triggerIdeation() {
  if (ideationRunning) {
    showAlert('Brainstorm is already running.');
    return;
  }
  try {
    await api('/api/ideate', { method: 'POST' });
    setIdeationRunning(true);
    startIdeationPoll();
  } catch (e) {
    if (e.message && e.message.includes('409')) {
      showAlert('Brainstorm is already running.');
    } else {
      showAlert('Error triggering brainstorm: ' + e.message);
    }
  }
}

// updateIdeationConfig updates local state from a config response object.
// Called by fetchConfig (in api.js / state.js) after the initial load.
function updateIdeationConfig(cfg) {
  ideation = !!cfg.ideation;
  const toggle = document.getElementById('ideation-toggle');
  if (toggle) toggle.checked = ideation;
  if (cfg.ideation_running) {
    setIdeationRunning(true);
    startIdeationPoll();
  } else {
    setIdeationRunning(false);
  }
}
