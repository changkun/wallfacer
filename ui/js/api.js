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
  tasksSource.onmessage = function(e) {
    tasksRetryDelay = 1000;
    try {
      const newTasks = JSON.parse(e.data);
      // When any task changes, invalidate cached behind-counts so waiting cards
      // immediately re-check how many commits they are behind the main branch.
      if (tasks && tasks.length > 0) {
        const prevById = new Map(tasks.map(t => [t.id, t]));
        const anyChanged = newTasks.some(t => {
          const prev = prevById.get(t.id);
          return !prev || prev.updated_at !== t.updated_at;
        });
        if (anyChanged) invalidateDiffBehindCounts();
      }
      tasks = newTasks;
      render();
    } catch (err) {
      console.error('tasks SSE parse error:', err);
    }
  };
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
    document.getElementById('refine-inline-model'),
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
