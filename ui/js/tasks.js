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
    const timeout = parseInt(document.getElementById('new-timeout').value, 10) || 5;
    await api('/api/tasks', { method: 'POST', body: JSON.stringify({ prompt, timeout }) });
    hideNewTaskForm();
    fetchTasks();
  } catch (e) {
    alert('Error creating task: ' + e.message);
  }
}

function showNewTaskForm() {
  document.getElementById('new-task-btn').classList.add('hidden');
  document.getElementById('new-task-form').classList.remove('hidden');
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
    alert('Error updating task: ' + e.message);
  }
}

async function toggleFreshStart(id, freshStart) {
  try {
    await api(`/api/tasks/${id}`, { method: 'PATCH', body: JSON.stringify({ fresh_start: freshStart }) });
  } catch (e) {
    alert('Error updating task: ' + e.message);
  }
}

// --- Task deletion ---

async function deleteTask(id) {
  try {
    await api(`/api/tasks/${id}`, { method: 'DELETE' });
    fetchTasks();
  } catch (e) {
    alert('Error deleting task: ' + e.message);
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
    alert('Error submitting feedback: ' + e.message);
  }
}

async function completeTask() {
  if (!currentTaskId) return;
  try {
    await api(`/api/tasks/${currentTaskId}/done`, { method: 'POST' });
    closeModal();
    fetchTasks();
  } catch (e) {
    alert('Error completing task: ' + e.message);
  }
}

// --- Retry & resume ---

async function retryTask() {
  const textarea = document.getElementById('modal-retry-prompt');
  const prompt = textarea.value.trim();
  if (!prompt || !currentTaskId) return;
  try {
    await api(`/api/tasks/${currentTaskId}`, {
      method: 'PATCH',
      body: JSON.stringify({ status: 'backlog', prompt }),
    });
    closeModal();
    fetchTasks();
  } catch (e) {
    alert('Error retrying task: ' + e.message);
  }
}

async function resumeTask() {
  if (!currentTaskId) return;
  try {
    await api(`/api/tasks/${currentTaskId}/resume`, { method: 'POST' });
    closeModal();
    fetchTasks();
  } catch (e) {
    alert('Error resuming task: ' + e.message);
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
    const timeout = parseInt(document.getElementById('modal-edit-timeout').value, 10) || 5;
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
    alert('Error cancelling task: ' + e.message);
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
    alert('Error archiving task: ' + e.message);
  }
}

async function unarchiveTask() {
  if (!currentTaskId) return;
  try {
    await api(`/api/tasks/${currentTaskId}/unarchive`, { method: 'POST' });
    closeModal();
    fetchTasks();
  } catch (e) {
    alert('Error unarchiving task: ' + e.message);
  }
}

// --- Sync with latest (rebase worktree onto default branch) ---

async function syncTask(id) {
  try {
    await api(`/api/tasks/${id}/sync`, { method: 'POST' });
    diffCache.delete(id);
    fetchTasks();
  } catch (e) {
    alert('Error syncing task: ' + e.message);
  }
}
