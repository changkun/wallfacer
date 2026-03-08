// --- API client ---

async function api(path, opts = {}) {
  const res = await fetch(path, {
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  });
  if (!res.ok && res.status !== 204) {
    const text = await res.text();
    throw new Error(text);
  }
  if (res.status === 204) return null;
  return res.json();
}

// --- Tasks SSE stream ---

function startTasksStream() {
  if (tasksSource) tasksSource.close();
  const url = showArchived ? '/api/tasks/stream?include_archived=true' : '/api/tasks/stream';
  tasksSource = new EventSource(url);

  // Initial full snapshot — replace the local tasks array and re-render.
  tasksSource.addEventListener('snapshot', function(e) {
    tasksRetryDelay = 1000;
    try {
      tasks = JSON.parse(e.data);
      render();
    } catch (err) {
      console.error('tasks SSE snapshot parse error:', err);
    }
  });

  // Single-task update — find by ID and replace in-place (or append if new).
  tasksSource.addEventListener('task-updated', function(e) {
    tasksRetryDelay = 1000;
    try {
      const task = JSON.parse(e.data);
      // If the task is archived and we're not showing archived tasks, treat as deleted.
      if (task.archived && !showArchived) {
        const idx = tasks.findIndex(t => t.id === task.id);
        if (idx >= 0) {
          tasks.splice(idx, 1);
          invalidateDiffBehindCounts();
          render();
        }
        return;
      }
      const idx = tasks.findIndex(t => t.id === task.id);
      if (idx >= 0) {
        tasks[idx] = task;
      } else {
        tasks.push(task);
      }
      invalidateDiffBehindCounts();
      render();
    } catch (err) {
      console.error('tasks SSE task-updated parse error:', err);
    }
  });

  // Single-task deletion — remove from local array.
  tasksSource.addEventListener('task-deleted', function(e) {
    tasksRetryDelay = 1000;
    try {
      const { id } = JSON.parse(e.data);
      const idx = tasks.findIndex(t => t.id === id);
      if (idx >= 0) {
        tasks.splice(idx, 1);
        invalidateDiffBehindCounts();
        render();
      }
    } catch (err) {
      console.error('tasks SSE task-deleted parse error:', err);
    }
  });

  tasksSource.onerror = function() {
    if (tasksSource.readyState === EventSource.CLOSED) {
      tasksSource = null;
      setTimeout(startTasksStream, tasksRetryDelay);
      tasksRetryDelay = Math.min(tasksRetryDelay * 2, 30000);
    }
  };
}

async function fetchTasks() {
  const url = showArchived ? '/api/tasks?include_archived=true' : '/api/tasks';
  tasks = await api(url);
  render();
}

function toggleShowArchived() {
  showArchived = document.getElementById('show-archived-toggle').checked;
  localStorage.setItem('wallfacer-show-archived', showArchived ? 'true' : 'false');
  startTasksStream();
}

// --- Autopilot ---

// Available sandbox list from server config.
let availableSandboxes = [];
let defaultSandbox = '';
let defaultSandboxByActivity = {};
const SANDBOX_ACTIVITY_KEYS = ['implementation', 'testing', 'refinement', 'title', 'oversight', 'commit_message', 'idea_agent'];

function sandboxDisplayName(id) {
  if (!id) return 'Default';
  if (id === 'claude') return 'Claude';
  if (id === 'codex') return 'Codex';
  return id.charAt(0).toUpperCase() + id.slice(1);
}

function populateSandboxSelects() {
  var selects = Array.from(document.querySelectorAll('select[data-sandbox-select]'));
  for (var sel of selects) {
    if (!sel) continue;
    var current = sel.value;
    var defaultText = sel.dataset.defaultText || 'Default';
    var includeDefault = sel.dataset.defaultOption !== 'false';
    sel.innerHTML = '';
    if (includeDefault) {
      var effectiveDefault = sel.dataset.defaultSandbox || '';
      if (!effectiveDefault) {
        var matched = SANDBOX_ACTIVITY_KEYS.find(function(key) { return sel.id.endsWith('-' + key); });
        if (matched) {
          effectiveDefault = defaultSandboxByActivity[matched] || defaultSandbox || '';
        } else {
          effectiveDefault = defaultSandbox || '';
        }
      }
      var suffix = effectiveDefault ? ' (' + sandboxDisplayName(effectiveDefault) + ')' : '';
      sel.innerHTML = '<option value="">' + defaultText + suffix + '</option>';
    }
    for (var s of availableSandboxes) {
      if (!s) continue;
      var opt = document.createElement('option');
      opt.value = s;
      opt.textContent = sandboxDisplayName(s);
      sel.appendChild(opt);
    }
    sel.value = current;
  }
}

function collectSandboxByActivity(prefix) {
  var out = {};
  SANDBOX_ACTIVITY_KEYS.forEach(function(key) {
    var el = document.getElementById(prefix + key);
    if (!el) return;
    var value = (el.value || '').trim();
    if (value) out[key] = value;
  });
  return out;
}

function applySandboxByActivity(prefix, values) {
  var data = values || {};
  SANDBOX_ACTIVITY_KEYS.forEach(function(key) {
    var el = document.getElementById(prefix + key);
    if (!el) return;
    el.value = data[key] || '';
  });
}

async function fetchConfig() {
  try {
    var cfg = await api('/api/config');
    autopilot = !!cfg.autopilot;
    var toggle = document.getElementById('autopilot-toggle');
    if (toggle) toggle.checked = autopilot;
    autotest = !!cfg.autotest;
    var atToggle = document.getElementById('autotest-toggle');
    if (atToggle) atToggle.checked = autotest;
    autosubmit = !!cfg.autosubmit;
    var asToggle = document.getElementById('autosubmit-toggle');
    if (asToggle) asToggle.checked = autosubmit;
    availableSandboxes = Array.isArray(cfg.sandboxes) ? cfg.sandboxes : [];
    defaultSandbox = cfg.default_sandbox || '';
    defaultSandboxByActivity = cfg.activity_sandboxes || {};
    populateSandboxSelects();
    // Sync ideation toggle and spinner state.
    if (typeof updateIdeationConfig === 'function') updateIdeationConfig(cfg);
  } catch (e) {
    console.error('fetchConfig:', e);
  }
}

async function toggleAutopilot() {
  var toggle = document.getElementById('autopilot-toggle');
  var enabled = toggle ? toggle.checked : !autopilot;
  try {
    var res = await api('/api/config', { method: 'PUT', body: JSON.stringify({ autopilot: enabled }) });
    autopilot = !!res.autopilot;
    if (toggle) toggle.checked = autopilot;
  } catch (e) {
    showAlert('Error toggling autopilot: ' + e.message);
    // Revert checkbox on failure.
    if (toggle) toggle.checked = autopilot;
  }
}

async function toggleAutotest() {
  var toggle = document.getElementById('autotest-toggle');
  var enabled = toggle ? toggle.checked : !autotest;
  try {
    var res = await api('/api/config', { method: 'PUT', body: JSON.stringify({ autotest: enabled }) });
    autotest = !!res.autotest;
    if (toggle) toggle.checked = autotest;
  } catch (e) {
    showAlert('Error toggling auto-test: ' + e.message);
    // Revert checkbox on failure.
    if (toggle) toggle.checked = autotest;
  }
}

async function toggleAutosubmit() {
  var toggle = document.getElementById('autosubmit-toggle');
  var enabled = toggle ? toggle.checked : !autosubmit;
  try {
    var res = await api('/api/config', { method: 'PUT', body: JSON.stringify({ autosubmit: enabled }) });
    autosubmit = !!res.autosubmit;
    if (toggle) toggle.checked = autosubmit;
  } catch (e) {
    showAlert('Error toggling auto-submit: ' + e.message);
    // Revert checkbox on failure.
    if (toggle) toggle.checked = autosubmit;
  }
}
