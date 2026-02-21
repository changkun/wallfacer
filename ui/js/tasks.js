const DEFAULT_TASK_TIMEOUT = 15; // minutes

// --- Task creation ---

async function createTask() {
  const textarea = document.getElementById('new-prompt');
  const prompt = textarea.value.trim();
  if (!prompt) {
    textarea.focus();
    textarea.style.borderColor = '#dc2626';
    setTimeout(() => textarea.style.borderColor = '', 2000);
    return;
  }
  try {
    const timeout = parseInt(document.getElementById('new-timeout').value, 10) || DEFAULT_TASK_TIMEOUT;
    await api('/api/tasks', { method: 'POST', body: JSON.stringify({ prompt, timeout }) });
    hideNewTaskForm();
    fetchTasks();
  } catch (e) {
    showAlert('Error creating task: ' + e.message);
  }
}

function showNewTaskForm() {
  document.getElementById('new-task-btn').classList.add('hidden');
  document.getElementById('new-task-form').classList.remove('hidden');
  document.getElementById('new-timeout').value = DEFAULT_TASK_TIMEOUT;
  const textarea = document.getElementById('new-prompt');
  textarea.value = '';
  textarea.style.height = '';
  textarea.focus();
}

function hideNewTaskForm() {
  document.getElementById('new-task-form').classList.add('hidden');
  document.getElementById('new-task-btn').classList.remove('hidden');
  const textarea = document.getElementById('new-prompt');
  textarea.value = '';
  textarea.style.height = '';
}

// --- Task status updates ---

async function updateTaskStatus(id, status) {
  try {
    await api(`/api/tasks/${id}`, { method: 'PATCH', body: JSON.stringify({ status }) });
    fetchTasks();
  } catch (e) {
    showAlert('Error updating task: ' + e.message);
  }
}

async function toggleFreshStart(id, freshStart) {
  try {
    await api(`/api/tasks/${id}`, { method: 'PATCH', body: JSON.stringify({ fresh_start: freshStart }) });
  } catch (e) {
    showAlert('Error updating task: ' + e.message);
  }
}

// --- Task deletion ---

async function deleteTask(id) {
  try {
    await api(`/api/tasks/${id}`, { method: 'DELETE' });
    fetchTasks();
  } catch (e) {
    showAlert('Error deleting task: ' + e.message);
  }
}

function deleteCurrentTask() {
  if (!currentTaskId) return;
  if (!confirm('Delete this task?')) return;
  deleteTask(currentTaskId);
  closeModal();
}

// --- Feedback & completion ---

async function submitFeedback() {
  const textarea = document.getElementById('modal-feedback');
  const message = textarea.value.trim();
  if (!message || !currentTaskId) return;
  try {
    await api(`/api/tasks/${currentTaskId}/feedback`, {
      method: 'POST',
      body: JSON.stringify({ message }),
    });
    textarea.value = '';
    closeModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error submitting feedback: ' + e.message);
  }
}

async function completeTask() {
  if (!currentTaskId) return;
  try {
    await api(`/api/tasks/${currentTaskId}/done`, { method: 'POST' });
    closeModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error completing task: ' + e.message);
  }
}

// --- Retry & resume ---

async function retryTask() {
  const textarea = document.getElementById('modal-retry-prompt');
  const prompt = textarea.value.trim();
  if (!prompt || !currentTaskId) return;
  try {
    const body = { status: 'backlog', prompt };
    const retryResumeRow = document.getElementById('modal-retry-resume-row');
    if (retryResumeRow && !retryResumeRow.classList.contains('hidden')) {
      body.fresh_start = !document.getElementById('modal-retry-resume').checked;
    }
    await api(`/api/tasks/${currentTaskId}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    });
    closeModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error retrying task: ' + e.message);
  }
}

async function resumeTask() {
  if (!currentTaskId) return;
  try {
    const timeoutEl = document.getElementById('modal-resume-timeout');
    const timeout = timeoutEl ? parseInt(timeoutEl.value, 10) || DEFAULT_TASK_TIMEOUT : DEFAULT_TASK_TIMEOUT;
    await api(`/api/tasks/${currentTaskId}/resume`, {
      method: 'POST',
      body: JSON.stringify({ timeout }),
    });
    closeModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error resuming task: ' + e.message);
  }
}

// --- Backlog editing ---

async function saveResumeOption(resume) {
  if (!currentTaskId) return;
  const statusEl = document.getElementById('modal-edit-status');
  try {
    await api(`/api/tasks/${currentTaskId}`, {
      method: 'PATCH',
      body: JSON.stringify({ fresh_start: !resume }),
    });
    statusEl.textContent = 'Saved';
    setTimeout(() => { if (statusEl.textContent === 'Saved') statusEl.textContent = ''; }, 1500);
  } catch (e) {
    statusEl.textContent = 'Save failed';
  }
}

function scheduleBacklogSave() {
  const statusEl = document.getElementById('modal-edit-status');
  statusEl.textContent = '';
  clearTimeout(editDebounce);
  editDebounce = setTimeout(async () => {
    if (!currentTaskId) return;
    const prompt = document.getElementById('modal-edit-prompt').value.trim();
    if (!prompt) return;
    const timeout = parseInt(document.getElementById('modal-edit-timeout').value, 10) || DEFAULT_TASK_TIMEOUT;
    try {
      await api(`/api/tasks/${currentTaskId}`, {
        method: 'PATCH',
        body: JSON.stringify({ prompt, timeout }),
      });
      statusEl.textContent = 'Saved';
      setTimeout(() => { if (statusEl.textContent === 'Saved') statusEl.textContent = ''; }, 1500);
      fetchTasks();
    } catch (e) {
      statusEl.textContent = 'Save failed';
    }
  }, 500);
}

document.getElementById('modal-edit-prompt').addEventListener('input', scheduleBacklogSave);
document.getElementById('modal-edit-timeout').addEventListener('change', scheduleBacklogSave);

// --- Cancel ---

async function cancelTask() {
  if (!currentTaskId) return;
  if (!confirm('Cancel this task? The sandbox will be cleaned up and all prepared changes discarded. History and logs will be preserved.')) return;
  try {
    await api(`/api/tasks/${currentTaskId}/cancel`, { method: 'POST' });
    closeModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error cancelling task: ' + e.message);
  }
}

// --- Archive / Unarchive ---

async function archiveTask() {
  if (!currentTaskId) return;
  try {
    await api(`/api/tasks/${currentTaskId}/archive`, { method: 'POST' });
    closeModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error archiving task: ' + e.message);
  }
}

async function unarchiveTask() {
  if (!currentTaskId) return;
  try {
    await api(`/api/tasks/${currentTaskId}/unarchive`, { method: 'POST' });
    closeModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error unarchiving task: ' + e.message);
  }
}

// --- Sync with latest (rebase worktree onto default branch) ---

async function syncTask(id) {
  try {
    await api(`/api/tasks/${id}/sync`, { method: 'POST' });
    diffCache.delete(id);
    fetchTasks();
  } catch (e) {
    showAlert('Error syncing task: ' + e.message);
  }
}

// --- Bulk title generation for tasks without a title ---

async function generateMissingTitles() {
  const statusEl = document.getElementById('generate-titles-status');
  const btn = document.querySelector('[onclick="generateMissingTitles()"]');
  const limit = document.getElementById('generate-titles-limit').value;

  btn.disabled = true;
  statusEl.innerHTML = '<span class="spinner" style="width:11px;height:11px;border-width:1.5px;vertical-align:middle;margin-right:4px;"></span>Checking tasks…';
  statusEl.style.color = 'var(--text-muted)';

  let interval = null;

  try {
    const params = new URLSearchParams({ limit });
    const res = await api(`/api/tasks/generate-titles?${params}`, { method: 'POST' });
    const { queued, total_without_title, task_ids } = res;

    if (queued === 0) {
      statusEl.textContent = total_without_title === 0
        ? 'All tasks already have titles.'
        : 'No tasks queued (limit reached or none found).';
      btn.disabled = false;
      return;
    }

    const pending = new Set(task_ids);
    let succeeded = 0;
    let failed = 0;
    const total = queued;
    const startTime = Date.now();
    const TIMEOUT_MS = 120_000;

    function updateStatus() {
      const done = succeeded + failed;
      const inFlight = pending.size > 0;
      const spinnerHtml = inFlight
        ? '<span class="spinner" style="width:11px;height:11px;border-width:1.5px;vertical-align:middle;margin-right:5px;"></span>'
        : '';
      const okHtml = succeeded > 0
        ? ` <span style="color:#16a34a">${succeeded} ok</span>`
        : '';
      const failHtml = failed > 0
        ? ` <span style="color:var(--danger,#dc2626)">${failed} failed</span>`
        : '';
      statusEl.style.color = 'var(--text-muted)';
      statusEl.innerHTML = `${spinnerHtml}${done}/${total} generated${okHtml}${failHtml}`;
    }

    updateStatus();

    interval = setInterval(() => {
      for (const id of [...pending]) {
        const task = tasks.find(t => t.id === id);
        if (task && task.title) {
          pending.delete(id);
          succeeded++;
        }
      }

      updateStatus();

      if (pending.size === 0) {
        clearInterval(interval);
        btn.disabled = false;
        return;
      }

      if (Date.now() - startTime > TIMEOUT_MS) {
        failed += pending.size;
        pending.clear();
        clearInterval(interval);
        updateStatus();
        btn.disabled = false;
      }
    }, 1000);

  } catch (e) {
    if (interval) clearInterval(interval);
    statusEl.textContent = 'Error: ' + e.message;
    statusEl.style.color = 'var(--danger, #dc2626)';
    btn.disabled = false;
  }
}
