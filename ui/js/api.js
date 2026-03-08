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

// Available model list from server config.
let availableModels = [];
let defaultModel = '';

function modelDisplayName(id) {
  if (!id) return 'Default';
  // e.g. "claude-sonnet-4-6-20250514" → "sonnet-4-6"
  var m = id.match(/^claude-(.+)-\d{8}$/);
  if (m) return m[1];
  // e.g. "bedrock/claude-sonnet-4.6" → "sonnet-4.6"
  m = id.match(/(?:^|\/)+claude-(.+)$/);
  if (m) return m[1];
  return id;
}

function populateModelSelects() {
  var selects = [
    document.getElementById('new-model'),
    document.getElementById('modal-edit-model'),
  ];
  for (var sel of selects) {
    if (!sel) continue;
    var current = sel.value;
    sel.innerHTML = '<option value="">Default' + (defaultModel ? ' (' + modelDisplayName(defaultModel) + ')' : '') + '</option>';
    for (var m of availableModels) {
      var opt = document.createElement('option');
      opt.value = m;
      opt.textContent = modelDisplayName(m);
      sel.appendChild(opt);
    }
    sel.value = current;
  }
}

async function fetchConfig() {
  try {
    var cfg = await api('/api/config');
    autopilot = !!cfg.autopilot;
    var toggle = document.getElementById('autopilot-toggle');
    if (toggle) toggle.checked = autopilot;
    availableModels = cfg.models || [];
    defaultModel = cfg.default_model || '';
    populateModelSelects();
    // Also populate the settings datalist for the default model input.
    var datalist = document.getElementById('env-model-list');
    if (datalist) {
      datalist.innerHTML = '';
      for (var m of availableModels) {
        var opt = document.createElement('option');
        opt.value = m;
        datalist.appendChild(opt);
      }
    }
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
