// --- API client ---

async function api(path, opts = {}) {
  const headers = { 'Content-Type': 'application/json', ...(opts.headers || {}) };
  const res = await fetch(path, {
    headers,
    signal: opts.signal,
    ...opts,
  });
  if (!res.ok && res.status !== 204) {
    const text = await res.text();
    throw new Error(text);
  }
  if (res.status === 204) return null;
  return res.json();
}

// --- Deep-link hash handling ---

// Called once after the first SSE snapshot. Checks window.location.hash for
// a task ID (and optional tab name) and opens the corresponding modal.
// Format: #<uuid> or #<uuid>/<tabName>
function _handleInitialHash() {
  if (_hashHandled) return;
  _hashHandled = true;
  const match = location.hash.match(/^#([0-9a-f-]{36})(?:\/([\w-]+))?$/);
  if (!match) return;
  const taskId = match[1];
  const tabName = match[2] || null;
  const task = tasks.find(t => t.id === taskId) || archivedTasks.find(t => t.id === taskId);
  if (!task) return;
  openModal(taskId).then(function() {
    if (tabName) {
      const rightTabs = ['implementation', 'testing', 'changes', 'spans', 'timeline'];
      const leftTabs = ['implementation', 'testing'];
      if (rightTabs.includes(tabName)) {
        setRightTab(tabName);
      } else if (leftTabs.includes(tabName)) {
        setLeftTab(tabName);
      }
    }
  });
}

// --- Tasks SSE stream ---

function sortArchivedByUpdatedDesc(items) {
  return items.sort(function(a, b) {
    const ad = new Date(a.updated_at).getTime();
    const bd = new Date(b.updated_at).getTime();
    if (bd !== ad) return bd - ad;
    if (a.id === b.id) return 0;
    return a.id > b.id ? -1 : 1;
  });
}

function resetArchivedWindow(shouldRender) {
  archivedTasks = [];
  archivedPage = { loadState: 'idle', hasMoreBefore: false, hasMoreAfter: false };
  if (shouldRender) scheduleRender();
}

function trimArchivedWindow(direction) {
  const pageSize = Math.max(1, archivedTasksPageSize || 20);
  const maxItems = pageSize * 3;
  if (archivedTasks.length <= maxItems) return;
  const overflow = archivedTasks.length - maxItems;
  if (direction === 'before') {
    archivedTasks = archivedTasks.slice(overflow);
    archivedPage.hasMoreAfter = true;
    return;
  }
  archivedTasks = archivedTasks.slice(0, maxItems);
  archivedPage.hasMoreBefore = true;
}

async function loadArchivedTasksPage(direction) {
  if (!showArchived) return;
  const dir = direction || 'initial';
  if (dir === 'before') {
    if (!archivedPage.hasMoreBefore || archivedPage.loadState !== 'idle' || archivedTasks.length === 0) return;
    archivedPage.loadState = 'loading-before';
  } else if (dir === 'after') {
    if (!archivedPage.hasMoreAfter || archivedPage.loadState !== 'idle' || archivedTasks.length === 0) return;
    archivedPage.loadState = 'loading-after';
  } else { // 'initial'
    if (archivedPage.loadState !== 'idle') return;
  }

  const pageSize = Math.max(1, archivedTasksPageSize || 20);
  let url = Routes.tasks.list() + '?include_archived=true&archived_page_size=' + encodeURIComponent(pageSize);
  if (dir === 'before' && archivedTasks.length > 0) {
    url += '&archived_before=' + encodeURIComponent(archivedTasks[archivedTasks.length - 1].id);
  }
  if (dir === 'after' && archivedTasks.length > 0) {
    url += '&archived_after=' + encodeURIComponent(archivedTasks[0].id);
  }

  try {
    const resp = await api(url);
    const page = Array.isArray(resp.tasks) ? resp.tasks : [];

    if (dir === 'initial') {
      archivedTasks = page;
    } else if (page.length > 0) {
      const seen = new Set(archivedTasks.map(function(t) { return t.id; }));
      const additions = page.filter(function(t) { return !seen.has(t.id); });
      if (dir === 'before') {
        archivedTasks = archivedTasks.concat(additions);
      } else {
        archivedTasks = additions.concat(archivedTasks);
      }
      sortArchivedByUpdatedDesc(archivedTasks);
      trimArchivedWindow(dir);
    }

    archivedPage.hasMoreBefore = !!resp.has_more_before;
    archivedPage.hasMoreAfter  = !!resp.has_more_after;
    scheduleRender();
  } catch (e) {
    console.error('loadArchivedTasksPage:', e);
  } finally {
    archivedPage.loadState = 'idle';
  }
}

function onDoneColumnScroll() {
  if (!showArchived) return;
  const col = document.getElementById('col-done');
  if (!col) return;
  const nearTop = col.scrollTop <= 80;
  const nearBottom = col.scrollTop + col.clientHeight >= col.scrollHeight - 160;
  if (nearBottom) {
    loadArchivedTasksPage('before');
    return;
  }
  if (nearTop) {
    loadArchivedTasksPage('after');
  }
}

function ensureArchivedScrollBinding() {
  if (archivedScrollHandlerBound) return;
  const col = document.getElementById('col-done');
  if (!col) return;
  col.addEventListener('scroll', onDoneColumnScroll);
  archivedScrollHandlerBound = true;
}

function startTasksStream() {
  if (tasksSource) tasksSource.close();
  ensureArchivedScrollBinding();

  // Build the stream URL. On reconnect, pass the last received event ID so
  // the server can replay only missed deltas instead of sending a full snapshot.
  let url = Routes.tasks.stream();
  if (lastTasksEventId !== null) {
    const sep = url.includes('?') ? '&' : '?';
    url += sep + 'last_event_id=' + encodeURIComponent(lastTasksEventId);
  }
  tasksSource = new EventSource(url);

  // Initial full snapshot — replace the local tasks array and re-render.
  // Also received when the server cannot replay (gap too old).
  tasksSource.addEventListener('snapshot', function(e) {
    tasksRetryDelay = 1000;
    if (e.lastEventId) lastTasksEventId = e.lastEventId;
    try {
      tasks = JSON.parse(e.data);
      if (showArchived) {
        loadArchivedTasksPage('initial');
      } else {
        resetArchivedWindow(false);
      }
      scheduleRender();
      _handleInitialHash();
    } catch (err) {
      console.error('tasks SSE snapshot parse error:', err);
    }
  });

  // Single-task update — find by ID and replace in-place (or append if new).
  // Received both from live stream and delta replay on reconnect.
  tasksSource.addEventListener('task-updated', function(e) {
    tasksRetryDelay = 1000;
    if (e.lastEventId) lastTasksEventId = e.lastEventId;
    try {
      const task = JSON.parse(e.data);
      const previousTask = tasks.find(function(t) { return t.id === task.id; }) ||
        archivedTasks.find(function(t) { return t.id === task.id; }) ||
        null;
      // If the task is archived and we're not showing archived tasks, treat as deleted.
      if (task.archived) {
        const idx = tasks.findIndex(t => t.id === task.id);
        if (idx >= 0) {
          tasks.splice(idx, 1);
        }
        const archivedIdx = archivedTasks.findIndex(t => t.id === task.id);
        if (showArchived) {
          if (archivedIdx >= 0) {
            archivedTasks[archivedIdx] = task;
          } else {
            archivedTasks.unshift(task);
            sortArchivedByUpdatedDesc(archivedTasks);
            trimArchivedWindow('after');
          }
          scheduleRender();
        } else if (archivedIdx >= 0) {
          archivedTasks.splice(archivedIdx, 1);
        }
        if (!showArchived) {
          invalidateDiffBehindCounts(task.id);
          scheduleRender();
        }
        return;
      }
      const archivedIdx = archivedTasks.findIndex(t => t.id === task.id);
      if (archivedIdx >= 0) archivedTasks.splice(archivedIdx, 1);
      const idx = tasks.findIndex(t => t.id === task.id);
      if (idx >= 0) {
        tasks[idx] = task;
      } else {
        tasks.push(task);
      }
      if (typeof cardOversightCache !== 'undefined' && cardOversightCache && typeof cardOversightCache.delete === 'function') {
        cardOversightCache.delete(task.id);
      }
      if (previousTask && previousTask.status !== task.status) {
        announceBoardStatus(`Task "${getTaskAccessibleTitle(task)}" is now ${formatTaskStatusLabel(task.status)}`);
      }
      invalidateDiffBehindCounts(task.id);
      scheduleRender();
    } catch (err) {
      console.error('tasks SSE task-updated parse error:', err);
    }
  });

  // Single-task deletion — remove from local array.
  // Received both from live stream and delta replay on reconnect.
  tasksSource.addEventListener('task-deleted', function(e) {
    tasksRetryDelay = 1000;
    if (e.lastEventId) lastTasksEventId = e.lastEventId;
    try {
      const { id } = JSON.parse(e.data);
      const idx = tasks.findIndex(t => t.id === id);
      if (idx >= 0) {
        tasks.splice(idx, 1);
      }
      const archivedIdx = archivedTasks.findIndex(t => t.id === id);
      if (archivedIdx >= 0) archivedTasks.splice(archivedIdx, 1);
      scheduleRender();
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
  ensureArchivedScrollBinding();
  tasks = await api(Routes.tasks.list());
  tasks = tasks.filter(function(t) { return !t.archived; });
  if (showArchived) {
    await loadArchivedTasksPage('initial');
  } else {
    resetArchivedWindow(false);
  }
  scheduleRender();
}

function toggleShowArchived() {
  showArchived = document.getElementById('show-archived-toggle').checked;
  localStorage.setItem('wallfacer-show-archived', showArchived ? 'true' : 'false');
  if (showArchived) {
    loadArchivedTasksPage('initial');
  } else {
    resetArchivedWindow(true);
  }
  startTasksStream();
}

// --- Autopilot ---

// Available sandbox list from server config.
let availableSandboxes = [];
let defaultSandbox = '';
let defaultSandboxByActivity = {};
let sandboxUsable = {};
let sandboxReasons = {};
let SANDBOX_ACTIVITY_KEYS = ['implementation', 'testing', 'refinement', 'title', 'oversight', 'commit_message', 'idea_agent'];

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
      var usable = sandboxUsable[s] !== false;
      opt.textContent = sandboxDisplayName(s) + (usable ? '' : ' (unavailable)');
      if (!usable) {
        opt.disabled = true;
        if (sandboxReasons[s]) opt.title = sandboxReasons[s];
      }
      sel.appendChild(opt);
    }
    sel.value = current;
    if (sel.selectedIndex === -1 || sel.value !== current) {
      sel.value = '';
    }
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

function configGetRoute() {
  if (typeof Routes !== 'undefined' && Routes.config && typeof Routes.config.get === 'function') {
    return Routes.config.get();
  }
  return '/' + 'api/config';
}

function configUpdateRoute() {
  if (typeof Routes !== 'undefined' && Routes.config && typeof Routes.config.update === 'function') {
    return Routes.config.update();
  }
  return '/' + 'api/config';
}

async function fetchConfig() {
  try {
    var cfg = await api(configGetRoute());
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
    sandboxUsable = cfg.sandbox_usable || {};
    sandboxReasons = cfg.sandbox_reasons || {};
    if (Array.isArray(cfg.sandbox_activities) && cfg.sandbox_activities.length > 0) {
      SANDBOX_ACTIVITY_KEYS = cfg.sandbox_activities;
    }
    if (typeof setBrainstormCategories === 'function') {
      setBrainstormCategories(cfg.ideation_categories || []);
    }
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
    var res = await api(configUpdateRoute(), { method: 'PUT', body: JSON.stringify({ autopilot: enabled }) });
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
    var res = await api(configUpdateRoute(), { method: 'PUT', body: JSON.stringify({ autotest: enabled }) });
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
    var res = await api(configUpdateRoute(), { method: 'PUT', body: JSON.stringify({ autosubmit: enabled }) });
    autosubmit = !!res.autosubmit;
    if (toggle) toggle.checked = autosubmit;
  } catch (e) {
    showAlert('Error toggling auto-submit: ' + e.message);
    // Revert checkbox on failure.
    if (toggle) toggle.checked = autosubmit;
  }
}
