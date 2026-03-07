// --- Task refinement via sandbox agent ---
//
// Flow:
//   1. User clicks "Start Refinement" → POST /api/tasks/{id}/refine
//   2. Sandbox agent runs read-only, produces a spec
//   3. Live logs stream via GET /api/tasks/{id}/refine/logs
//   4. On completion, result appears in an editable textarea
//   5. User optionally edits and clicks "Apply as Prompt"

let refineTaskId = null;
let refineLogsAbort = null; // AbortController for the live log stream

// updateRefineUI re-renders the refinement panel based on the current task state.
// Called whenever the modal opens or the task object changes via SSE.
function updateRefineUI(task) {
  if (!task || task.status !== 'backlog') return;

  const job = task.current_refinement;

  const startBtn   = document.getElementById('refine-start-btn');
  const cancelBtn  = document.getElementById('refine-cancel-btn');
  const running    = document.getElementById('refine-running');
  const resultSec  = document.getElementById('refine-result-section');
  const errorSec   = document.getElementById('refine-error-section');
  const resultTA   = document.getElementById('refine-result-prompt');
  const errorMsg   = document.getElementById('refine-error-msg');

  // Determine UI state from job status.
  if (!job) {
    showRefineIdle(startBtn, cancelBtn, running, resultSec, errorSec);
    return;
  }

  if (job.status === 'running') {
    startBtn.classList.add('hidden');
    cancelBtn.classList.remove('hidden');
    running.classList.remove('hidden');
    resultSec.classList.add('hidden');
    errorSec.classList.add('hidden');

    // Attach log stream if this is the active task and not already streaming.
    if (refineTaskId === task.id && !refineLogsAbort) {
      startRefineLogStream(task.id);
    }
    return;
  }

  if (job.status === 'done') {
    startBtn.classList.remove('hidden');
    cancelBtn.classList.add('hidden');
    running.classList.add('hidden');
    resultSec.classList.remove('hidden');
    errorSec.classList.add('hidden');
    stopRefineLogStream();

    // Only populate the textarea if it is empty or this is the first population.
    if (resultTA.dataset.jobId !== job.id) {
      resultTA.value = job.result || '';
      resultTA.dataset.jobId = job.id;
    }
    return;
  }

  if (job.status === 'failed') {
    showRefineIdle(startBtn, cancelBtn, running, resultSec, errorSec);
    errorSec.classList.remove('hidden');
    errorMsg.textContent = 'Refinement failed: ' + (job.error || 'unknown error');
    stopRefineLogStream();
    return;
  }
}

function showRefineIdle(startBtn, cancelBtn, running, resultSec, errorSec) {
  startBtn.classList.remove('hidden');
  cancelBtn.classList.add('hidden');
  running.classList.add('hidden');
  resultSec.classList.add('hidden');
  errorSec.classList.add('hidden');
}

// startRefinement is called by the "Start" button.
async function startRefinement() {
  if (!currentTaskId) return;

  // If refinement is already running, ignore the click.
  const task = tasks.find(t => t.id === currentTaskId);
  if (task && task.current_refinement && task.current_refinement.status === 'running') return;

  refineTaskId = currentTaskId;

  // Clear prior log output.
  const logsEl = document.getElementById('refine-logs');
  if (logsEl) logsEl.textContent = '';

  // Clear prior result textarea job-id so result gets populated fresh.
  const resultTA = document.getElementById('refine-result-prompt');
  if (resultTA) delete resultTA.dataset.jobId;

  try {
    await api(`/api/tasks/${currentTaskId}/refine`, { method: 'POST' });
    // SSE task stream will push the updated task; updateRefineUI handles the rest.
  } catch (e) {
    const errorSec = document.getElementById('refine-error-section');
    const errorMsg = document.getElementById('refine-error-msg');
    if (errorSec) errorSec.classList.remove('hidden');
    if (errorMsg) errorMsg.textContent = 'Failed to start refinement: ' + e.message;
  }
}

// cancelRefinement is called by the "Cancel" button.
async function cancelRefinement() {
  if (!refineTaskId) return;
  stopRefineLogStream();
  try {
    await api(`/api/tasks/${refineTaskId}/refine`, { method: 'DELETE' });
  } catch (e) {
    // Ignore — SSE will reflect the updated state.
  }
}

// startRefineLogStream opens a streaming fetch to /api/tasks/{id}/refine/logs
// and appends lines to the log display area.
function startRefineLogStream(taskId) {
  if (refineLogsAbort) return; // already streaming
  refineLogsAbort = new AbortController();

  const logsEl = document.getElementById('refine-logs');

  fetch(`/api/tasks/${taskId}/refine/logs`, { signal: refineLogsAbort.signal })
    .then(async resp => {
      if (resp.status === 204) {
        // Container already done.
        refineLogsAbort = null;
        return;
      }
      if (!resp.ok || !resp.body) {
        refineLogsAbort = null;
        return;
      }
      const reader = resp.body.getReader();
      const decoder = new TextDecoder();
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        const text = decoder.decode(value, { stream: true });
        if (logsEl) {
          logsEl.textContent += text;
          logsEl.scrollTop = logsEl.scrollHeight;
        }
      }
      refineLogsAbort = null;
    })
    .catch(err => {
      if (err.name !== 'AbortError') {
        console.warn('refine log stream error:', err);
      }
      refineLogsAbort = null;
    });
}

// stopRefineLogStream aborts any active log stream.
function stopRefineLogStream() {
  if (refineLogsAbort) {
    refineLogsAbort.abort();
    refineLogsAbort = null;
  }
}

// resetRefinePanel resets the panel state when the modal closes or switches tasks.
function resetRefinePanel() {
  refineTaskId = null;
  stopRefineLogStream();
  const resultTA = document.getElementById('refine-result-prompt');
  if (resultTA) delete resultTA.dataset.jobId;
  const logsEl = document.getElementById('refine-logs');
  if (logsEl) logsEl.textContent = '';
}

// applyRefinement POSTs the (possibly edited) spec as the new task prompt.
async function applyRefinement() {
  if (!currentTaskId) return;
  const resultTA = document.getElementById('refine-result-prompt');
  const newPrompt = resultTA ? resultTA.value.trim() : '';
  if (!newPrompt) {
    showAlert('The refined prompt cannot be empty.');
    return;
  }

  try {
    // Save settings changes (model, timeout, mount_worktrees) alongside the apply.
    const model = document.getElementById('modal-edit-model')?.value || '';
    const timeout = parseInt(document.getElementById('modal-edit-timeout')?.value, 10) || DEFAULT_TASK_TIMEOUT;
    const mountWorktrees = document.getElementById('modal-edit-mount-worktrees')?.checked || false;
    await api(`/api/tasks/${currentTaskId}`, {
      method: 'PATCH',
      body: JSON.stringify({ model, timeout, mount_worktrees: mountWorktrees }),
    });

    await api(`/api/tasks/${currentTaskId}/refine/apply`, {
      method: 'POST',
      body: JSON.stringify({ prompt: newPrompt }),
    });

    closeModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error applying refinement: ' + e.message);
  }
}

// renderRefineHistory populates the history section from task.refine_sessions.
function renderRefineHistory(task) {
  const section = document.getElementById('refine-history-section');
  const list = document.getElementById('refine-history-list');
  const sessions = (task.refine_sessions || []);
  if (sessions.length === 0) {
    section.classList.add('hidden');
    return;
  }
  section.classList.remove('hidden');
  list.innerHTML = sessions.map((s, i) => {
    const date = new Date(s.created_at).toLocaleString();
    const previewPrompt = s.start_prompt || '';
    const resultPrompt = s.result_prompt || '';
    const sandboxResult = s.result || '';
    return `<details class="refine-history-entry">
      <summary class="refine-history-summary">
        <span class="text-xs text-v-muted">#${i + 1} · ${escapeHtml(date)}</span>
      </summary>
      <div style="padding:8px 0 0 0;">
        <div class="text-xs text-v-muted" style="margin-bottom:4px;">Starting prompt:</div>
        <pre class="code-block text-xs" style="white-space:pre-wrap;word-break:break-word;opacity:0.7;">${escapeHtml(previewPrompt)}</pre>
        ${sandboxResult ? `
        <details style="margin-top:8px;">
          <summary class="text-xs text-v-muted" style="cursor:pointer;">Sandbox spec (before editing)</summary>
          <pre class="code-block text-xs" style="white-space:pre-wrap;word-break:break-word;margin-top:4px;opacity:0.8;">${escapeHtml(sandboxResult)}</pre>
        </details>
        ` : ''}
        ${resultPrompt ? `
        <div class="text-xs text-v-muted" style="margin-top:8px;margin-bottom:4px;">Applied prompt:</div>
        <pre class="code-block text-xs" style="white-space:pre-wrap;word-break:break-word;">${escapeHtml(resultPrompt)}</pre>
        <button class="btn btn-ghost text-xs" style="margin-top:6px;" onclick="revertToHistoryPrompt(${i})">Revert to this version</button>
        ` : ''}
      </div>
    </details>`;
  }).join('');
}

// revertToHistoryPrompt loads a previous session's applied prompt into the result textarea.
function revertToHistoryPrompt(sessionIndex) {
  const task = tasks.find(t => t.id === currentTaskId);
  if (!task || !task.refine_sessions) return;
  const session = task.refine_sessions[sessionIndex];
  if (!session || !session.result_prompt) return;

  const resultTA = document.getElementById('refine-result-prompt');
  if (!resultTA) return;

  // Show the result section so the user can apply.
  document.getElementById('refine-result-section').classList.remove('hidden');
  resultTA.value = session.result_prompt;
  delete resultTA.dataset.jobId;
}
