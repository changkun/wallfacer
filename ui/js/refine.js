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
    const idleDesc = document.getElementById('refine-idle-desc');
    if (idleDesc) idleDesc.classList.add('hidden');
    const instrSec = document.getElementById('refine-instructions-section');
    if (instrSec) instrSec.classList.add('hidden');

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
    const idleDesc = document.getElementById('refine-idle-desc');
    if (idleDesc) idleDesc.classList.add('hidden');
    const instrSec = document.getElementById('refine-instructions-section');
    if (instrSec) instrSec.classList.add('hidden');
    stopRefineLogStream();

    // Only populate the textarea if it is empty or this is the first population.
    if (resultTA.dataset.jobId !== job.id) {
      resultTA.value = job.result || '';
      resultTA.dataset.jobId = job.id;
    }

    // Show the dismiss button so user can skip applying the refinement.
    const dismissBtn = document.getElementById('refine-dismiss-btn');
    if (dismissBtn) dismissBtn.classList.remove('hidden');
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
  const idleDesc = document.getElementById('refine-idle-desc');
  if (idleDesc) idleDesc.classList.remove('hidden');
  const instrSec = document.getElementById('refine-instructions-section');
  if (instrSec) instrSec.classList.remove('hidden');
  const dismissBtn = document.getElementById('refine-dismiss-btn');
  if (dismissBtn) dismissBtn.classList.add('hidden');
}

// startRefinement is called by the "Start" button.
async function startRefinement() {
  if (!currentTaskId) return;

  // If refinement is already running, ignore the click.
  const currentTask = tasks.find(t => t.id === currentTaskId);
  if (currentTask && currentTask.current_refinement && currentTask.current_refinement.status === 'running') return;

  refineTaskId = currentTaskId;

  // Clear prior log output and reset mode.
  refineRawLogBuffer = '';
  refineLogsMode = 'pretty';
  setRefineLogsMode('pretty');
  const logsEl = document.getElementById('refine-logs');
  if (logsEl) logsEl.innerHTML = '';

  // Clear prior result textarea job-id so result gets populated fresh.
  const resultTA = document.getElementById('refine-result-prompt');
  if (resultTA) delete resultTA.dataset.jobId;

  try {
    const userInstructions = document.getElementById('refine-user-instructions')?.value.trim() || '';
    await api(task(currentTaskId).refine(), {
      method: 'POST',
      body: JSON.stringify({ user_instructions: userInstructions }),
    });
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
    await api(task(refineTaskId).refine(), { method: 'DELETE' });
  } catch (e) {
    // Ignore — SSE will reflect the updated state.
  }
}

// --- Refine log render scheduler ---
// Coalesces rapid chunk arrivals into a single paint per animation frame,
// mirroring the scheduleLogRender() pattern used by modal-logs.js.
let _refineLogRenderPending = false;
function scheduleRefineLogRender() {
  if (_refineLogRenderPending) return;
  _refineLogRenderPending = true;
  requestAnimationFrame(function() {
    _refineLogRenderPending = false;
    renderRefineLogs();
  });
}

// renderRefineLogs re-renders the refine log area from refineRawLogBuffer.
function renderRefineLogs() {
  const logsEl = document.getElementById('refine-logs');
  if (!logsEl) return;
  // Read scroll state before mutating the DOM to avoid a forced synchronous layout
  // between the read and the subsequent innerHTML write.
  const atBottom = logsEl.scrollHeight - logsEl.scrollTop - logsEl.clientHeight < 80;
  if (refineLogsMode === 'pretty') {
    logsEl.innerHTML = renderPrettyLogs(refineRawLogBuffer);
  } else {
    logsEl.textContent = refineRawLogBuffer.replace(/\x1b\[[0-9;]*[a-zA-Z]/g, '');
  }
  if (atBottom) {
    // Defer the scroll-to-bottom to the next frame so the browser can batch
    // the layout triggered by the innerHTML write with the scroll update.
    requestAnimationFrame(function() {
      logsEl.scrollTop = logsEl.scrollHeight;
    });
  }
}

// setRefineLogsMode switches between 'pretty' and 'raw' and re-renders.
function setRefineLogsMode(mode) {
  refineLogsMode = mode;
  ['pretty', 'raw'].forEach(function(m) {
    const tab = document.getElementById('refine-logs-tab-' + m);
    if (tab) tab.classList.toggle('active', m === mode);
  });
  renderRefineLogs();
}

// startRefineLogStream opens a streaming fetch to the refine/logs endpoint
// and accumulates chunks into refineRawLogBuffer for pretty/raw rendering.
function startRefineLogStream(taskId) {
  if (refineLogsAbort) return; // already streaming
  refineLogsAbort = new AbortController();

  const decoder = new TextDecoder();

  fetch(task(taskId).refineLogs(), { signal: refineLogsAbort.signal })
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
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        refineRawLogBuffer += decoder.decode(value, { stream: true });
        scheduleRefineLogRender();
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
  // Reset all sub-elements individually to avoid errors when elements are absent.
  const startBtn  = document.getElementById('refine-start-btn');
  const cancelBtn = document.getElementById('refine-cancel-btn');
  const running   = document.getElementById('refine-running');
  const resultSec = document.getElementById('refine-result-section');
  const errorSec  = document.getElementById('refine-error-section');
  if (startBtn)  startBtn.classList.remove('hidden');
  if (cancelBtn) cancelBtn.classList.add('hidden');
  if (running)   running.classList.add('hidden');
  if (resultSec) resultSec.classList.add('hidden');
  if (errorSec)  errorSec.classList.add('hidden');
  const idleDesc = document.getElementById('refine-idle-desc');
  if (idleDesc) idleDesc.classList.remove('hidden');
  const instrSec = document.getElementById('refine-instructions-section');
  if (instrSec) instrSec.classList.remove('hidden');
  const instrTA = document.getElementById('refine-user-instructions');
  if (instrTA) instrTA.value = '';
  const resultTA = document.getElementById('refine-result-prompt');
  if (resultTA) delete resultTA.dataset.jobId;
  const dismissBtn = document.getElementById('refine-dismiss-btn');
  if (dismissBtn) dismissBtn.classList.add('hidden');
  refineRawLogBuffer = '';
  refineLogsMode = 'pretty';
  ['pretty', 'raw'].forEach(function(m) {
    const tab = document.getElementById('refine-logs-tab-' + m);
    if (tab) tab.classList.toggle('active', m === 'pretty');
  });
  const logsEl = document.getElementById('refine-logs');
  if (logsEl) logsEl.innerHTML = '';
}

// dismissRefinement clears the refinement result without applying it, allowing
// the task to be started with the original prompt.
async function dismissRefinement() {
  if (!currentTaskId) return;
  try {
    await api(task(currentTaskId).refineDismiss(), { method: 'POST' });
    closeModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error dismissing refinement: ' + e.message);
  }
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
    // Save settings changes (sandbox, timeout, mount_worktrees) alongside the apply.
    const sandbox = document.getElementById('modal-edit-sandbox')?.value || '';
    const sandboxByActivity = collectSandboxByActivity('modal-edit-sandbox-');
    const timeout = parseInt(document.getElementById('modal-edit-timeout')?.value, 10) || DEFAULT_TASK_TIMEOUT;
    const mountWorktrees = document.getElementById('modal-edit-mount-worktrees')?.checked || false;
    await api(task(currentTaskId).update(), {
      method: 'PATCH',
      body: JSON.stringify({ sandbox, sandbox_by_activity: sandboxByActivity, timeout, mount_worktrees: mountWorktrees }),
    });

    await api(task(currentTaskId).refineApply(), {
      method: 'POST',
      body: JSON.stringify({ prompt: newPrompt }),
    });

    await fetchTasks();
    openModal(currentTaskId);
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
  const currentTask = tasks.find(t => t.id === currentTaskId);
  if (!currentTask || !currentTask.refine_sessions) return;
  const session = currentTask.refine_sessions[sessionIndex];
  if (!session || !session.result_prompt) return;

  const resultTA = document.getElementById('refine-result-prompt');
  if (!resultTA) return;

  // Show the result section so the user can apply.
  document.getElementById('refine-result-section').classList.remove('hidden');
  resultTA.value = session.result_prompt;
  delete resultTA.dataset.jobId;
}
