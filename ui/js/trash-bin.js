const TRASH_BIN_RETENTION_DAYS = 7;

let _trashBinInitialized = false;
let _trashBinRestoreTimers = [];

function getTrashBinTitle(task) {
  if (task && task.title) return task.title;
  if (task && task.prompt) return task.prompt.length > 60 ? task.prompt.slice(0, 60) + '\u2026' : task.prompt;
  return task && task.id ? task.id : 'Untitled task';
}

function getTrashBinRemainingDays(task) {
  var updatedAt = task && task.updated_at ? Date.parse(task.updated_at) : NaN;
  if (!Number.isFinite(updatedAt)) return 0;
  var daysElapsed = Math.floor((Date.now() - updatedAt) / 86400000);
  return Math.max(0, TRASH_BIN_RETENTION_DAYS - daysElapsed);
}

function getTrashBinRemainingDaysLabel(task) {
  var days = getTrashBinRemainingDays(task);
  return days === 1 ? '1 day remaining' : days + ' days remaining';
}

function getTrashBinDeletedAgo(task) {
  var updatedAt = task && task.updated_at ? Date.parse(task.updated_at) : NaN;
  if (!Number.isFinite(updatedAt)) return 'unknown';
  var seconds = Math.floor((Date.now() - updatedAt) / 1000);
  if (seconds < 60) return 'just now';
  if (seconds < 3600) {
    var minutes = Math.floor(seconds / 60);
    return minutes === 1 ? '1 minute ago' : minutes + ' minutes ago';
  }
  if (seconds < 86400) {
    var hours = Math.floor(seconds / 3600);
    return hours === 1 ? '1 hour ago' : hours + ' hours ago';
  }
  var days = Math.floor(seconds / 86400);
  return days === 1 ? '1 day ago' : days + ' days ago';
}

function setTrashBinLoading(isLoading) {
  var loading = document.getElementById('trash-bin-loading');
  var list = document.getElementById('trash-bin-list');
  if (loading) loading.classList.toggle('hidden', !isLoading);
  if (list) list.classList.toggle('hidden', !!isLoading);
}

function clearTrashBinError() {
  var error = document.getElementById('trash-bin-error');
  var message = document.getElementById('trash-bin-error-message');
  if (message) message.textContent = '';
  if (error) error.classList.add('hidden');
}

function showTrashBinError(message) {
  var error = document.getElementById('trash-bin-error');
  var messageEl = document.getElementById('trash-bin-error-message');
  if (messageEl) messageEl.textContent = message || 'Failed to load deleted tasks.';
  if (error) error.classList.remove('hidden');
}

function showTrashBinEmpty(show) {
  var empty = document.getElementById('trash-bin-empty');
  if (empty) empty.classList.toggle('hidden', !show);
}

function closeTrashBin() {
  var panel = document.getElementById('trash-bin-panel');
  var button = document.getElementById('trash-bin-btn');
  if (panel) panel.classList.add('hidden');
  if (button) button.setAttribute('aria-expanded', 'false');
}

function openTrashBin() {
  var panel = document.getElementById('trash-bin-panel');
  var button = document.getElementById('trash-bin-btn');
  if (panel) panel.classList.remove('hidden');
  if (button) button.setAttribute('aria-expanded', 'true');
  return loadDeletedTasks();
}

function toggleTrashBin() {
  var panel = document.getElementById('trash-bin-panel');
  if (!panel) return Promise.resolve();
  if (panel.classList.contains('hidden')) return openTrashBin();
  closeTrashBin();
  return Promise.resolve();
}

function showTrashBinToast(message) {
  var toast = document.getElementById('trash-bin-toast');
  if (!toast) return;
  toast.textContent = message;
  toast.classList.remove('hidden');
  while (_trashBinRestoreTimers.length > 0) {
    clearTimeout(_trashBinRestoreTimers.pop());
  }
  _trashBinRestoreTimers.push(setTimeout(function() {
    toast.classList.add('hidden');
  }, 2200));
}

function renderDeletedTasks(items) {
  var list = document.getElementById('trash-bin-list');
  if (!list) return;
  list.innerHTML = '';
  showTrashBinEmpty(!Array.isArray(items) || items.length === 0);
  if (!Array.isArray(items) || items.length === 0) return;

  items.forEach(function(taskItem) {
    var row = document.createElement('div');
    row.className = 'trash-bin-row';
    row.setAttribute('role', 'listitem');
    row.dataset.taskId = taskItem.id;

    var main = document.createElement('div');
    main.className = 'trash-bin-row__main';

    var title = document.createElement('div');
    title.className = 'trash-bin-row__title';
    title.textContent = getTrashBinTitle(taskItem);

    var meta = document.createElement('div');
    meta.className = 'trash-bin-row__meta';

    var badge = document.createElement('span');
    badge.className = 'badge badge-' + (taskItem.status || 'backlog');
    badge.textContent = formatTaskStatusLabel(taskItem.status);

    var deleted = document.createElement('span');
    deleted.textContent = getTrashBinDeletedAgo(taskItem);

    var retention = document.createElement('span');
    retention.className = 'trash-bin-row__retention';
    retention.textContent = getTrashBinRemainingDaysLabel(taskItem);

    meta.appendChild(badge);
    meta.appendChild(deleted);
    meta.appendChild(retention);
    main.appendChild(title);
    main.appendChild(meta);

    var restore = document.createElement('button');
    restore.type = 'button';
    restore.className = 'trash-bin-row__restore';
    restore.textContent = 'Restore';
    restore.addEventListener('click', function() {
      restoreDeletedTask(taskItem.id, row, restore, getTrashBinTitle(taskItem));
    });

    row.appendChild(main);
    row.appendChild(restore);
    list.appendChild(row);
  });
}

async function loadDeletedTasks() {
  clearTrashBinError();
  showTrashBinEmpty(false);
  setTrashBinLoading(true);
  try {
    var deletedTasks = await api(Routes.tasks.listDeleted());
    renderDeletedTasks(Array.isArray(deletedTasks) ? deletedTasks : []);
  } catch (e) {
    showTrashBinError('Error loading trash: ' + (e && e.message ? e.message : e));
    renderDeletedTasks([]);
  } finally {
    setTrashBinLoading(false);
  }
}

async function restoreDeletedTask(id, row, button, title) {
  if (button) {
    button.disabled = true;
    button.textContent = 'Restoring...';
  }
  clearTrashBinError();
  try {
    await api(task(id).restore(), { method: 'POST' });
    if (row && typeof row.remove === 'function') row.remove();
    var list = document.getElementById('trash-bin-list');
    var hasRows = list && list.children && list.children.length > 0;
    showTrashBinEmpty(!hasRows);
    showTrashBinToast('Restored "' + title + '"');
  } catch (e) {
    if (button) {
      button.disabled = false;
      button.textContent = 'Restore';
    }
    showTrashBinError('Error restoring task: ' + (e && e.message ? e.message : e));
  }
}

function initTrashBin() {
  if (_trashBinInitialized) return;
  _trashBinInitialized = true;

  var button = document.getElementById('trash-bin-btn');
  var closeButton = document.getElementById('trash-bin-close-btn');
  var dismissButton = document.getElementById('trash-bin-error-dismiss');

  if (button) {
    button.addEventListener('click', function() {
      toggleTrashBin();
    });
  }
  if (closeButton) {
    closeButton.addEventListener('click', function() {
      closeTrashBin();
    });
  }
  if (dismissButton) {
    dismissButton.addEventListener('click', function() {
      clearTrashBinError();
    });
  }
}
