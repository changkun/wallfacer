const DEFAULT_TASK_TIMEOUT = 60; // minutes

function setActivityOverrideDefaultSandbox(prefix, sandbox) {
  SANDBOX_ACTIVITY_KEYS.forEach(function(key) {
    var el = document.getElementById(prefix + key);
    if (!el) return;
    if (sandbox) {
      el.dataset.defaultSandbox = sandbox;
    } else {
      delete el.dataset.defaultSandbox;
    }
  });
  populateSandboxSelects();
}

function bindTaskSandboxInheritance(selectId, prefix) {
  var el = document.getElementById(selectId);
  if (!el || el.dataset.inheritanceBound === 'true') return;
  el.dataset.inheritanceBound = 'true';
  el.addEventListener('change', function() {
    setActivityOverrideDefaultSandbox(prefix, (el.value || '').trim());
  });
}

// --- Dependency picker helpers ---

/**
 * Updates the chips display in a dep-picker based on currently checked items.
 * Also fires the picker's data-onchange callback if set.
 */
function updateDepPickerChips(wrapperId, fireCallback) {
  var wrap = document.getElementById(wrapperId);
  if (!wrap) return;
  var chipsEl = wrap.querySelector('.dep-picker-chips');
  var checked = wrap.querySelectorAll('.dep-picker-item input[type=checkbox]:checked');
  if (checked.length === 0) {
    chipsEl.innerHTML = '<span class="dep-picker-placeholder">None</span>';
  } else {
    chipsEl.innerHTML = '';
    checked.forEach(function(cb) {
      var text = cb.closest('.dep-picker-item').querySelector('.dep-picker-item-text').textContent;
      var chip = document.createElement('span');
      chip.className = 'dep-picker-chip';
      chip.title = text;
      chip.textContent = text;
      chipsEl.appendChild(chip);
    });
  }
  if (fireCallback) {
    var cbName = wrap.dataset.onchange;
    if (cbName && typeof window[cbName] === 'function') window[cbName]();
  }
}

/**
 * Populates a dep-picker with tasks as checkbox items.
 * Excludes the task with excludeId (null to include all).
 * Pre-selects UUIDs in selectedIds array.
 */
function populateDependsOnPicker(wrapperId, excludeId, selectedIds) {
  var wrap = document.getElementById(wrapperId);
  if (!wrap) return;
  var list = wrap.querySelector('.dep-picker-list');
  var search = wrap.querySelector('.dep-picker-search');
  if (search) search.value = '';
  list.innerHTML = '';
  var statusPriority = { in_progress: 0, waiting: 1, backlog: 2, done: 3 };
  var candidates = tasks
    .filter(function(t) { return t.id !== excludeId; })
    .slice()
    .sort(function(a, b) {
      var pa = statusPriority[a.status] !== undefined ? statusPriority[a.status] : 4;
      var pb = statusPriority[b.status] !== undefined ? statusPriority[b.status] : 4;
      return pa - pb;
    });
  if (candidates.length === 0) {
    list.innerHTML = '<div class="dep-picker-empty">No other tasks</div>';
    updateDepPickerChips(wrapperId, false);
    return;
  }
  candidates.forEach(function(t) {
    var label = t.title || (t.prompt.length > 60 ? t.prompt.slice(0, 60) + '\u2026' : t.prompt);
    var status = t.status === 'in_progress' ? 'in progress' : t.status;
    var isSelected = Array.isArray(selectedIds) && selectedIds.indexOf(t.id) !== -1;
    var item = document.createElement('label');
    item.className = 'dep-picker-item' + (isSelected ? ' selected' : '');
    var cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.value = t.id;
    cb.checked = isSelected;
    var textSpan = document.createElement('span');
    textSpan.className = 'dep-picker-item-text';
    textSpan.textContent = label;
    var badge = document.createElement('span');
    badge.className = 'badge badge-' + t.status;
    badge.textContent = status;
    item.appendChild(cb);
    item.appendChild(textSpan);
    item.appendChild(badge);
    cb.addEventListener('change', function() {
      item.classList.toggle('selected', cb.checked);
      updateDepPickerChips(wrapperId, true);
    });
    list.appendChild(item);
  });
  updateDepPickerChips(wrapperId, false);
}

/** Returns array of selected task IDs from a dep-picker. */
function getDepPickerValues(wrapperId) {
  var wrap = document.getElementById(wrapperId);
  if (!wrap) return [];
  return Array.from(wrap.querySelectorAll('.dep-picker-item input[type=checkbox]:checked'))
    .map(function(cb) { return cb.value; });
}

/** Toggles the dep-picker dropdown open/closed. */
function toggleDepPicker(wrapperId) {
  var wrap = document.getElementById(wrapperId);
  var isOpen = wrap.classList.contains('open');
  // Close all open pickers
  document.querySelectorAll('.dep-picker.open').forEach(function(p) {
    p.classList.remove('open');
    p.querySelector('.dep-picker-dropdown').style.display = 'none';
  });
  if (!isOpen) {
    wrap.classList.add('open');
    wrap.querySelector('.dep-picker-dropdown').style.display = '';
    var search = wrap.querySelector('.dep-picker-search');
    if (search) { filterDepPicker(search); search.focus(); }
  }
}

/** Filters dep-picker items based on search input. */
function filterDepPicker(inputEl) {
  var search = inputEl.value.toLowerCase();
  var list = inputEl.closest('.dep-picker-dropdown').querySelector('.dep-picker-list');
  list.querySelectorAll('.dep-picker-item').forEach(function(item) {
    var text = item.querySelector('.dep-picker-item-text').textContent.toLowerCase();
    item.style.display = text.includes(search) ? '' : 'none';
  });
}

// Close any open dep-picker when clicking outside
document.addEventListener('click', function(e) {
  if (!e.target.closest('.dep-picker')) {
    document.querySelectorAll('.dep-picker.open').forEach(function(p) {
      p.classList.remove('open');
      p.querySelector('.dep-picker-dropdown').style.display = 'none';
    });
  }
});

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
    const mount_worktrees = document.getElementById('new-mount-worktrees').checked;
    const sandbox = document.getElementById('new-sandbox').value;
    const sandbox_by_activity = collectSandboxByActivity('new-sandbox-');
    const newTask = await api(Routes.tasks.create(), { method: 'POST', body: JSON.stringify({ prompt, timeout, mount_worktrees, sandbox, sandbox_by_activity }) });
    const dependsOn = getDepPickerValues('new-depends-on-picker');
    if (dependsOn.length > 0 && newTask && newTask.id) {
      await api(task(newTask.id).update(), { method: 'PATCH', body: JSON.stringify({ depends_on: dependsOn }) });
    }
    localStorage.removeItem('wallfacer-new-task-draft');
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
  const draft = localStorage.getItem('wallfacer-new-task-draft') || '';
  textarea.value = draft;
  textarea.style.height = draft ? textarea.scrollHeight + 'px' : '';
  textarea.focus();
  const sandboxSelect = document.getElementById('new-sandbox');
  bindTaskSandboxInheritance('new-sandbox', 'new-sandbox-');
  if (sandboxSelect) {
    sandboxSelect.value = defaultSandbox || '';
  }
  // Do not prefill per-activity overrides on new tasks. Empty values inherit
  // from task default sandbox, preventing stale global overrides (e.g. claude)
  // from shadowing an explicit task sandbox (e.g. codex).
  applySandboxByActivity('new-sandbox-', {});
  setActivityOverrideDefaultSandbox('new-sandbox-', (sandboxSelect && sandboxSelect.value) ? sandboxSelect.value : '');
  var depsRow = document.getElementById('new-depends-on-row');
  populateDependsOnPicker('new-depends-on-picker', null, []);
  if (depsRow) depsRow.style.display = tasks.length > 0 ? '' : 'none';
}

function hideNewTaskForm() {
  document.getElementById('new-task-form').classList.add('hidden');
  document.getElementById('new-task-btn').classList.remove('hidden');
  const textarea = document.getElementById('new-prompt');
  textarea.value = '';
  textarea.style.height = '';
  document.getElementById('new-mount-worktrees').checked = false;
  const sandboxSelect = document.getElementById('new-sandbox');
  if (sandboxSelect) {
    sandboxSelect.value = defaultSandbox || '';
  }
  applySandboxByActivity('new-sandbox-', {});
  setActivityOverrideDefaultSandbox('new-sandbox-', (sandboxSelect && sandboxSelect.value) ? sandboxSelect.value : '');
  var depPicker = document.getElementById('new-depends-on-picker');
  if (depPicker) {
    depPicker.querySelector('.dep-picker-list').innerHTML = '';
    depPicker.querySelector('.dep-picker-chips').innerHTML = '<span class="dep-picker-placeholder">None</span>';
    depPicker.classList.remove('open');
    depPicker.querySelector('.dep-picker-dropdown').style.display = 'none';
  }
}

// --- Task status updates ---

async function updateTaskStatus(id, status) {
  try {
    await api(task(id).update(), { method: 'PATCH', body: JSON.stringify({ status }) });
    fetchTasks();
  } catch (e) {
    showAlert('Error updating task: ' + e.message);
  }
}

async function toggleFreshStart(id, freshStart) {
  try {
    await api(task(id).update(), { method: 'PATCH', body: JSON.stringify({ fresh_start: freshStart }) });
  } catch (e) {
    showAlert('Error updating task: ' + e.message);
  }
}

// --- Task deletion ---

async function deleteTask(id) {
  try {
    await api(task(id).delete(), { method: 'DELETE' });
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
    await api(task(currentTaskId).feedback(), {
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
    await api(task(currentTaskId).done(), { method: 'POST' });
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
    await api(task(currentTaskId).update(), {
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
    await api(task(currentTaskId).resume(), {
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
    await api(task(currentTaskId).update(), {
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
    const mount_worktrees = document.getElementById('modal-edit-mount-worktrees').checked;
    const sandbox = document.getElementById('modal-edit-sandbox').value;
    const sandbox_by_activity = collectSandboxByActivity('modal-edit-sandbox-');
    const depends_on = getDepPickerValues('modal-edit-depends-on-picker');
    const patchBody = { prompt, timeout, mount_worktrees, sandbox, sandbox_by_activity, depends_on };
    try {
      await api(task(currentTaskId).update(), {
        method: 'PATCH',
        body: JSON.stringify(patchBody),
      });
      statusEl.textContent = 'Saved';
      setTimeout(() => { if (statusEl.textContent === 'Saved') statusEl.textContent = ''; }, 1500);
      // Update rendered prompt on the left panel.
      document.getElementById('modal-prompt-rendered').innerHTML = renderMarkdown(prompt);
      document.getElementById('modal-prompt').textContent = prompt;
      fetchTasks();
    } catch (e) {
      statusEl.textContent = 'Save failed';
    }
  }, 500);
}

document.getElementById('modal-edit-prompt').addEventListener('input', scheduleBacklogSave);
document.getElementById('modal-edit-timeout').addEventListener('change', scheduleBacklogSave);

// --- Start (backlog → in_progress) ---

async function startTask() {
  if (!currentTaskId) return;
  try {
    await api(task(currentTaskId).update(), { method: 'PATCH', body: JSON.stringify({ status: 'in_progress' }) });
    closeModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error starting task: ' + e.message);
  }
}

// --- Cancel ---

async function cancelTask() {
  if (!currentTaskId) return;
  if (!confirm('Cancel this task? The sandbox will be cleaned up and all prepared changes discarded. History and logs will be preserved.')) return;
  try {
    await api(task(currentTaskId).cancel(), { method: 'POST' });
    closeModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error cancelling task: ' + e.message);
  }
}

// --- Archive / Unarchive ---

async function archiveAllDone() {
  try {
    const result = await api(Routes.tasks.archiveDone(), { method: 'POST' });
    fetchTasks();
  } catch (e) {
    showAlert('Error archiving tasks: ' + e.message);
  }
}

async function archiveTask() {
  if (!currentTaskId) return;
  try {
    await api(task(currentTaskId).archive(), { method: 'POST' });
    closeModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error archiving task: ' + e.message);
  }
}

async function unarchiveTask() {
  if (!currentTaskId) return;
  try {
    await api(task(currentTaskId).unarchive(), { method: 'POST' });
    closeModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error unarchiving task: ' + e.message);
  }
}

// --- Quick card actions (no modal required) ---

async function quickDoneTask(id) {
  try {
    await api(task(id).done(), { method: 'POST' });
    fetchTasks();
  } catch (e) {
    showAlert('Error completing task: ' + e.message);
  }
}

async function quickResumeTask(id, timeout) {
  try {
    await api(task(id).resume(), { method: 'POST', body: JSON.stringify({ timeout }) });
    fetchTasks();
  } catch (e) {
    showAlert('Error resuming task: ' + e.message);
  }
}

async function quickRetryTask(id) {
  try {
    await api(task(id).update(), { method: 'PATCH', body: JSON.stringify({ status: 'backlog' }) });
    fetchTasks();
  } catch (e) {
    showAlert('Error retrying task: ' + e.message);
  }
}

// --- Test agent ---

function toggleTestSection() {
  const section = document.getElementById('modal-test-section');
  section.classList.toggle('hidden');
  if (!section.classList.contains('hidden')) {
    document.getElementById('modal-test-criteria').focus();
  }
}

async function runTestTask() {
  if (!currentTaskId) return;
  const criteria = document.getElementById('modal-test-criteria').value.trim();
  try {
    const res = await api(task(currentTaskId).test(), {
      method: 'POST',
      body: JSON.stringify({ criteria }),
    });
    closeModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error starting test verification: ' + e.message);
  }
}

async function quickTestTask(id) {
  try {
    await api(task(id).test(), {
      method: 'POST',
      body: JSON.stringify({ criteria: '' }),
    });
    fetchTasks();
  } catch (e) {
    showAlert('Error starting test verification: ' + e.message);
  }
}

// --- Sync with latest (rebase worktree onto default branch) ---

async function syncTask(id) {
  try {
    await api(task(id).sync(), { method: 'POST' });
    diffCache.delete(id);
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
    const res = await api(Routes.tasks.generateTitles() + '?' + params, { method: 'POST' });
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
        const t = tasks.find(t => t.id === id);
        if (t && t.title) {
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

// --- Bulk oversight generation for tasks without a summary ---

async function generateMissingOversight() {
  const statusEl = document.getElementById('generate-oversight-status');
  const btn = document.querySelector('[onclick="generateMissingOversight()"]');
  const limit = document.getElementById('generate-oversight-limit').value;

  btn.disabled = true;
  statusEl.innerHTML = '<span class="spinner" style="width:11px;height:11px;border-width:1.5px;vertical-align:middle;margin-right:4px;"></span>Checking tasks…';
  statusEl.style.color = 'var(--text-muted)';

  let interval = null;

  try {
    const params = new URLSearchParams({ limit });
    const res = await api(Routes.tasks.generateOversight() + '?' + params, { method: 'POST' });
    const { queued, total_without_oversight, task_ids } = res;

    if (queued === 0) {
      statusEl.textContent = total_without_oversight === 0
        ? 'All eligible tasks already have oversight summaries.'
        : 'No tasks queued (limit reached or none found).';
      btn.disabled = false;
      return;
    }

    const pending = new Set(task_ids);
    let succeeded = 0;
    let failed = 0;
    const total = queued;
    const startTime = Date.now();
    const TIMEOUT_MS = 300_000; // 5 min — oversight takes longer than titles

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

    interval = setInterval(async () => {
      if (Date.now() - startTime > TIMEOUT_MS) {
        failed += pending.size;
        pending.clear();
        clearInterval(interval);
        updateStatus();
        btn.disabled = false;
        return;
      }

      const checks = [...pending].map(id =>
        api(task(id).oversight()).then(o => ({ id, status: o.status })).catch(() => ({ id, status: 'error' }))
      );
      const results = await Promise.all(checks);
      for (const { id, status } of results) {
        if (status === 'ready') {
          pending.delete(id);
          succeeded++;
        } else if (status === 'failed' || status === 'error') {
          pending.delete(id);
          failed++;
        }
      }

      updateStatus();

      if (pending.size === 0) {
        clearInterval(interval);
        btn.disabled = false;
      }
    }, 3000);

  } catch (e) {
    if (interval) clearInterval(interval);
    statusEl.textContent = 'Error: ' + e.message;
    statusEl.style.color = 'var(--danger, #dc2626)';
    btn.disabled = false;
  }
}
