// --- Brainstorm / Ideation agent ---

// Client-side ideation state (mirrors server config).
let ideation = false;

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
    if (toggle) toggle.checked = ideation;
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

// updateIdeationConfig updates local state from a config response object.
// Called by fetchConfig after the initial load.
function updateIdeationConfig(cfg) {
  ideation = !!cfg.ideation;
  const toggle = document.getElementById('ideation-toggle');
  if (toggle) toggle.checked = ideation;
}
