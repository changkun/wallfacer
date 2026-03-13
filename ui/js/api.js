/** @typedef {import('./generated/types.js').Task} Task */
/** @typedef {import('./generated/types.js').TaskEvent} TaskEvent */
/** @typedef {import('./generated/types.js').TaskStatus} TaskStatus */
/** @typedef {import('./generated/types.js').TaskUsage} TaskUsage */
/** @typedef {import('./generated/types.js').TaskKind} TaskKind */
/** @typedef {import('./generated/types.js').EventType} EventType */
/** @typedef {import('./generated/types.js').RefinementSession} RefinementSession */
/** @typedef {import('./generated/types.js').ExecutionEnvironment} ExecutionEnvironment */

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

function cloneArchivedPage(page) {
  return {
    loadState: page && page.loadState ? page.loadState : 'idle',
    hasMoreBefore: !!(page && page.hasMoreBefore),
    hasMoreAfter: !!(page && page.hasMoreAfter),
  };
}

function createTasksState(state) {
  const source = state || {};
  return {
    tasks: Array.isArray(source.tasks) ? source.tasks.slice() : [],
    archivedTasks: Array.isArray(source.archivedTasks) ? source.archivedTasks.slice() : [],
    archivedPage: cloneArchivedPage(source.archivedPage),
  };
}

function trimArchivedWindowState(state, direction, pageSize) {
  const next = createTasksState(state);
  const size = Math.max(1, pageSize || 20);
  const maxItems = size * 3;
  if (next.archivedTasks.length <= maxItems) return next;
  const overflow = next.archivedTasks.length - maxItems;
  if (direction === 'before') {
    next.archivedTasks = next.archivedTasks.slice(overflow);
    next.archivedPage.hasMoreAfter = true;
    return next;
  }
  next.archivedTasks = next.archivedTasks.slice(0, maxItems);
  next.archivedPage.hasMoreBefore = true;
  return next;
}

function applyTasksSnapshot(state, snapshot) {
  const next = createTasksState(state);
  next.tasks = Array.isArray(snapshot) ? snapshot.slice() : [];
  return next;
}

function applyTaskDeleted(state, payload) {
  const id = payload && payload.id;
  const next = createTasksState(state);
  next.tasks = next.tasks.filter(function(t) { return t.id !== id; });
  next.archivedTasks = next.archivedTasks.filter(function(t) { return t.id !== id; });
  return next;
}

function applyTaskUpdated(state, task, opts) {
  const options = opts || {};
  const next = createTasksState(state);
  const showArchivedTasks = !!options.showArchived;
  const pageSize = Math.max(1, options.pageSize || 20);
  const previousTask = next.tasks.find(function(t) { return t.id === task.id; }) ||
    next.archivedTasks.find(function(t) { return t.id === task.id; }) ||
    null;

  next.tasks = next.tasks.filter(function(t) { return t.id !== task.id; });
  next.archivedTasks = next.archivedTasks.filter(function(t) { return t.id !== task.id; });

  if (task.archived) {
    if (showArchivedTasks) {
      next.archivedTasks.unshift(task);
      sortArchivedByUpdatedDesc(next.archivedTasks);
      return {
        state: trimArchivedWindowState(next, 'after', pageSize),
        previousTask,
      };
    }
    return { state: next, previousTask };
  }

  next.tasks.push(task);
  return { state: next, previousTask };
}

function mergeArchivedTasksPage(state, resp, direction, pageSize) {
  const dir = direction || 'initial';
  const page = resp && Array.isArray(resp.tasks) ? resp.tasks : [];
  const next = createTasksState(state);

  if (dir === 'initial') {
    next.archivedTasks = page.slice();
  } else if (page.length > 0) {
    const seen = new Set(next.archivedTasks.map(function(t) { return t.id; }));
    const additions = page.filter(function(t) { return !seen.has(t.id); });
    if (additions.length > 0) {
      if (dir === 'before') {
        next.archivedTasks = next.archivedTasks.concat(additions);
      } else {
        next.archivedTasks = additions.concat(next.archivedTasks);
      }
      sortArchivedByUpdatedDesc(next.archivedTasks);
      const trimmed = trimArchivedWindowState(next, dir, pageSize);
      next.archivedTasks = trimmed.archivedTasks;
      next.archivedPage = trimmed.archivedPage;
    }
  }

  next.archivedPage.hasMoreBefore = !!(resp && resp.has_more_before);
  next.archivedPage.hasMoreAfter = !!(resp && resp.has_more_after);
  return next;
}

function buildTasksStreamUrl(baseUrl, eventId) {
  if (eventId === null || typeof eventId === 'undefined') return baseUrl;
  const sep = baseUrl.includes('?') ? '&' : '?';
  return baseUrl + sep + 'last_event_id=' + encodeURIComponent(eventId);
}

function resetArchivedWindow(shouldRender) {
  archivedTasks = [];
  archivedPage = { loadState: 'idle', hasMoreBefore: false, hasMoreAfter: false };
  if (shouldRender) scheduleRender();
}

function trimArchivedWindow(direction) {
  const next = trimArchivedWindowState({
    tasks: tasks,
    archivedTasks: archivedTasks,
    archivedPage: archivedPage,
  }, direction, archivedTasksPageSize);
  archivedTasks = next.archivedTasks;
  archivedPage = next.archivedPage;
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
    const next = mergeArchivedTasksPage({
      tasks: tasks,
      archivedTasks: archivedTasks,
      archivedPage: archivedPage,
    }, resp, dir, pageSize);
    archivedTasks = next.archivedTasks;
    archivedPage = next.archivedPage;
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
  if (!activeWorkspaces || activeWorkspaces.length === 0) {
    return;
  }
  if (tasksSource) tasksSource.close();
  ensureArchivedScrollBinding();

  // Build the stream URL. On reconnect, pass the last received event ID so
  // the server can replay only missed deltas instead of sending a full snapshot.
  const url = buildTasksStreamUrl(Routes.tasks.stream(), lastTasksEventId);
  tasksSource = new EventSource(url);

  // Initial full snapshot — replace the local tasks array and re-render.
  // Also received when the server cannot replay (gap too old).
  tasksSource.addEventListener('snapshot', function(e) {
    tasksRetryDelay = 1000;
    if (e.lastEventId) lastTasksEventId = e.lastEventId;
    try {
      const next = applyTasksSnapshot({
        tasks: tasks,
        archivedTasks: archivedTasks,
        archivedPage: archivedPage,
      }, JSON.parse(e.data));
      tasks = next.tasks;
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
      const reduced = applyTaskUpdated({
        tasks: tasks,
        archivedTasks: archivedTasks,
        archivedPage: archivedPage,
      }, task, {
        showArchived: showArchived,
        pageSize: archivedTasksPageSize,
      });
      tasks = reduced.state.tasks;
      archivedTasks = reduced.state.archivedTasks;
      archivedPage = reduced.state.archivedPage;
      if (task.archived) {
        if (!showArchived) invalidateDiffBehindCounts(task.id);
        scheduleRender();
        return;
      }
      if (typeof cardOversightCache !== 'undefined' && cardOversightCache && typeof cardOversightCache.delete === 'function') {
        cardOversightCache.delete(task.id);
      }
      if (reduced.previousTask && reduced.previousTask.status !== task.status) {
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
      const next = applyTaskDeleted({
        tasks: tasks,
        archivedTasks: archivedTasks,
        archivedPage: archivedPage,
      }, JSON.parse(e.data));
      tasks = next.tasks;
      archivedTasks = next.archivedTasks;
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

function stopTasksStream() {
  if (tasksSource) tasksSource.close();
  tasksSource = null;
}

function stopGitStream() {
  if (gitStatusSource) gitStatusSource.close();
  gitStatusSource = null;
}

function resetBoardState() {
  tasks = [];
  archivedTasks = [];
  archivedPage = { loadState: 'idle', hasMoreBefore: false, hasMoreAfter: false };
  gitStatuses = [];
  lastTasksEventId = null;
  rawLogBuffer = '';
  testRawLogBuffer = '';
  renderWorkspaces();
  scheduleRender();
}

function restartActiveStreams() {
  stopTasksStream();
  stopGitStream();
  if (activeWorkspaces && activeWorkspaces.length > 0) {
    startGitStream();
    startTasksStream();
  }
}

/**
 * Fetches the current non-archived task list from the server.
 * @returns {Promise<Array.<Task>>}
 */
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

/**
 * waitForTaskDelta resolves once the SSE stream delivers a task-updated or
 * task-deleted event for taskId, or falls back to fetchTasks() if the stream
 * is absent or the event does not arrive within timeoutMs (default 2 000 ms).
 * Used after mutations so the board refreshes from the live SSE event rather
 * than a redundant HTTP round-trip.
 */
function waitForTaskDelta(taskId, timeoutMs) {
  var ms = typeof timeoutMs === 'number' ? timeoutMs : 2000;
  if (!tasksSource || tasksSource.readyState === EventSource.CLOSED) {
    return fetchTasks();
  }
  return new Promise(function(resolve) {
    var done = false;
    var timer = null;

    function finish(useFetch) {
      if (done) return;
      done = true;
      if (timer !== null) { clearTimeout(timer); timer = null; }
      if (tasksSource) {
        tasksSource.removeEventListener('task-updated', onUpdated);
        tasksSource.removeEventListener('task-deleted', onDeleted);
      }
      if (useFetch) {
        fetchTasks().then(resolve, resolve);
      } else {
        resolve();
      }
    }

    function onUpdated(e) {
      try {
        var t = JSON.parse(e.data);
        if (t && t.id === taskId) finish(false);
      } catch (_) {}
    }

    function onDeleted(e) {
      try {
        var payload = JSON.parse(e.data);
        if (payload && payload.id === taskId) finish(false);
      } catch (_) {}
    }

    tasksSource.addEventListener('task-updated', onUpdated);
    tasksSource.addEventListener('task-deleted', onDeleted);
    timer = setTimeout(function() { finish(true); }, ms);
  });
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
    activeWorkspaces = Array.isArray(cfg.workspaces) ? cfg.workspaces.slice() : [];
    workspaceGroups = Array.isArray(cfg.workspace_groups) ? cfg.workspace_groups.slice() : [];
    workspaceBrowserPath = cfg.workspace_browser_path || activeWorkspaces[0] || workspaceBrowserPath || '';
    workspacePickerRequired = activeWorkspaces.length === 0;
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
    renderWorkspaceSelectionSummary();
    renderWorkspaceGroups();
    if (workspacePickerRequired) {
      stopTasksStream();
      stopGitStream();
      resetBoardState();
      showWorkspacePicker(true);
    } else {
      hideWorkspacePicker();
      restartActiveStreams();
    }
    // Sync ideation toggle and spinner state.
    if (typeof updateIdeationConfig === 'function') updateIdeationConfig(cfg);
  } catch (e) {
    console.error('fetchConfig:', e);
  }
}

function workspaceBrowseRoute() {
  return Routes.workspaces.browse();
}

function workspaceUpdateRoute() {
  return Routes.workspaces.update();
}

function showWorkspacePicker(required) {
  var modal = document.getElementById('workspace-picker');
  var closeBtn = document.getElementById('workspace-picker-close');
  var filterInput = document.getElementById('workspace-browser-filter');
  if (!modal) return;
  workspacePickerRequired = !!required;
  if (closeBtn) closeBtn.style.display = workspacePickerRequired ? 'none' : '';
  modal.classList.remove('hidden');
  modal.classList.add('flex');
  workspaceBrowserFilterQuery = '';
  if (filterInput) filterInput.value = '';
  if (!workspaceSelectionDraft.length && activeWorkspaces.length) {
    workspaceSelectionDraft = activeWorkspaces.slice();
  }
  renderWorkspaceSelectionDraft();
  browseWorkspaces(workspaceBrowserPath || '');
}

function hideWorkspacePicker() {
  if (workspacePickerRequired) return;
  var modal = document.getElementById('workspace-picker');
  if (!modal) return;
  modal.classList.add('hidden');
  modal.classList.remove('flex');
}

function renderWorkspaceSelectionSummary() {
  var el = document.getElementById('settings-workspace-list');
  if (!el) return;
  if (!activeWorkspaces.length) {
    el.innerHTML = '<div style="color:var(--text-muted);">No workspaces configured.</div>';
    return;
  }
  el.innerHTML = activeWorkspaces.map(function(path) {
    return '<div style="font-family:monospace;font-size:11px;padding:6px 8px;border:1px solid var(--border);border-radius:6px;background:var(--bg-elevated);">' + escapeHtml(path) + '</div>';
  }).join('');
}

function workspaceGroupLabel(group) {
  if (!group || !Array.isArray(group.workspaces) || !group.workspaces.length) return 'Empty group';
  var names = group.workspaces.map(function(path) {
    var clean = String(path || '').replace(/[\\/]+$/, '');
    var parts = clean.split(/[\\/]/);
    return parts[parts.length - 1] || clean;
  });
  return names.join(' + ');
}

function renderWorkspaceGroups() {
  var el = document.getElementById('settings-workspace-groups');
  if (!el) return;
  if (!workspaceGroups.length) {
    el.innerHTML = '<div style="color:var(--text-muted);font-size:11px;">Saved workspace groups will appear here after you switch boards.</div>';
    return;
  }
  el.innerHTML = workspaceGroups.map(function(group, index) {
    var paths = Array.isArray(group.workspaces) ? group.workspaces : [];
    var active = JSON.stringify(paths) === JSON.stringify(activeWorkspaces);
    return '<div style="border:1px solid var(--border);border-radius:8px;padding:8px;background:var(--bg-elevated);display:flex;flex-direction:column;gap:8px;">' +
      '<div style="display:flex;align-items:center;justify-content:space-between;gap:8px;">' +
      '<div style="font-size:12px;font-weight:600;">' + escapeHtml(workspaceGroupLabel(group)) + (active ? ' <span style="font-size:10px;color:var(--text-muted);font-weight:500;">Current</span>' : '') + '</div>' +
      '<div style="display:flex;gap:6px;align-items:center;">' +
      '<button type="button" class="btn-icon" style="font-size:11px;padding:3px 8px;" onclick="useWorkspaceGroup(' + index + ')">Use</button>' +
      '<button type="button" class="btn-ghost" style="font-size:11px;padding:3px 8px;" onclick="editWorkspaceGroup(' + index + ')">Edit</button>' +
      '<button type="button" class="btn-ghost" style="font-size:11px;padding:3px 8px;" onclick="deleteWorkspaceGroup(' + index + ')">Remove</button>' +
      '</div>' +
      '</div>' +
      '<div style="display:flex;flex-direction:column;gap:4px;">' +
      paths.map(function(path) {
        return '<div style="font-family:monospace;font-size:11px;color:var(--text-muted);word-break:break-all;">' + escapeHtml(path) + '</div>';
      }).join('') +
      '</div>' +
      '</div>';
  }).join('');
}

function renderWorkspaceSelectionDraft() {
  var el = document.getElementById('workspace-selection-list');
  if (!el) return;
  if (!workspaceSelectionDraft.length) {
    el.innerHTML = '<div style="font-size:11px;color:var(--text-muted);">No folders selected.</div>';
    return;
  }
  el.innerHTML = workspaceSelectionDraft.map(function(path) {
    return '<div style="display:flex;align-items:center;justify-content:space-between;gap:8px;border:1px solid var(--border);border-radius:8px;padding:8px;background:var(--bg-elevated);">' +
      '<span style="font-family:monospace;font-size:11px;word-break:break-all;">' + escapeHtml(path) + '</span>' +
      '<button type="button" class="btn-ghost" data-workspace-path="' + escapeHtml(path) + '" onclick="removeWorkspaceSelection(this.dataset.workspacePath)">Remove</button>' +
      '</div>';
  }).join('');
}

function renderWorkspaceBrowser() {
  var crumb = document.getElementById('workspace-browser-breadcrumb');
  var list = document.getElementById('workspace-browser-list');
  var entriesEl = document.getElementById('workspace-browser-entries');
  var visibleEntries = getVisibleWorkspaceBrowserEntries();
  if (crumb) crumb.textContent = workspaceBrowserPath || '';
  if (!list || !entriesEl) return;
  if (!visibleEntries.length) {
    entriesEl.innerHTML = '<div style="font-size:11px;color:var(--text-muted);padding:8px;">' + (workspaceBrowserFilterQuery ? 'No matching directories found.' : 'No directories found.') + '</div>';
    return;
  }
  entriesEl.innerHTML = visibleEntries.map(function(entry, index) {
    var active = index === workspaceBrowserFocusIndex;
    return '<button type="button" data-workspace-entry-index="' + index + '" onclick="selectWorkspaceBrowserEntry(' + index + ')" ondblclick="openWorkspaceBrowserEntry(' + index + ')" style="display:flex;width:100%;align-items:center;justify-content:space-between;gap:8px;padding:8px 10px;border:none;border-radius:6px;background:' + (active ? 'var(--bg-input)' : 'transparent') + ';color:inherit;cursor:pointer;text-align:left;">' +
      '<span style="font-size:12px;">' + escapeHtml(entry.name) + '</span>' +
      '<span style="font-size:10px;color:var(--text-muted);">' + (entry.is_git_repo ? 'git repo' : 'folder') + '</span>' +
      '</button>';
  }).join('');
}

function getVisibleWorkspaceBrowserEntries() {
  var query = (workspaceBrowserFilterQuery || '').trim().toLowerCase();
  if (!query) return workspaceBrowserEntries.slice();
  return workspaceBrowserEntries.filter(function(entry) {
    return entry && ((entry.name || '').toLowerCase().includes(query) || (entry.path || '').toLowerCase().includes(query));
  });
}

function setWorkspaceBrowserFilter(query) {
  workspaceBrowserFilterQuery = (query || '').trim();
  var visibleEntries = getVisibleWorkspaceBrowserEntries();
  workspaceBrowserFocusIndex = visibleEntries.length ? 0 : -1;
  renderWorkspaceBrowser();
}

function workspaceBrowserIncludeHidden() {
  var toggle = document.getElementById('workspace-browser-include-hidden');
  return !!(toggle && toggle.checked);
}

async function browseWorkspaces(path) {
  var pathInput = document.getElementById('workspace-browser-path');
  var status = document.getElementById('workspace-browser-status');
  var nextPath = typeof path === 'string' ? path : (pathInput ? pathInput.value.trim() : '');
  try {
    if (status) status.textContent = 'Loading...';
    var url = workspaceBrowseRoute();
    var query = [];
    if (nextPath) {
      query.push('path=' + encodeURIComponent(nextPath));
    }
    if (workspaceBrowserIncludeHidden()) {
      query.push('include_hidden=true');
    }
    if (query.length > 0) {
      url += '?' + query.join('&');
    }
    var resp = await api(url);
    workspaceBrowserPath = resp.path || nextPath || '';
    workspaceBrowserEntries = Array.isArray(resp.entries) ? resp.entries : [];
    workspaceBrowserFocusIndex = getVisibleWorkspaceBrowserEntries().length ? 0 : -1;
    if (pathInput) pathInput.value = workspaceBrowserPath;
    if (status) status.textContent = workspaceBrowserEntries.length ? 'Double-click a folder to enter it. Filter to narrow the list; press Enter to add the selected folder.' : 'No subdirectories found.';
    renderWorkspaceBrowser();
  } catch (e) {
    if (status) status.textContent = e.message;
    workspaceBrowserEntries = [];
    workspaceBrowserFocusIndex = -1;
    renderWorkspaceBrowser();
  }
}

function toggleWorkspaceBrowserHidden() {
  browseWorkspaces(workspaceBrowserPath || '');
}

function workspaceBrowserPathKeydown(event) {
  if (event.key === 'Enter') {
    event.preventDefault();
    browseWorkspaces();
  }
}

function workspaceBrowserListKeydown(event) {
  var visibleEntries = getVisibleWorkspaceBrowserEntries();
  if (!visibleEntries.length) return;
  if (event.key === 'ArrowDown') {
    event.preventDefault();
    workspaceBrowserFocusIndex = Math.min(visibleEntries.length - 1, workspaceBrowserFocusIndex + 1);
    renderWorkspaceBrowser();
  } else if (event.key === 'ArrowUp') {
    event.preventDefault();
    workspaceBrowserFocusIndex = Math.max(0, workspaceBrowserFocusIndex - 1);
    renderWorkspaceBrowser();
  } else if (event.key === 'Enter') {
    event.preventDefault();
    if (event.metaKey || event.ctrlKey) {
      openWorkspaceBrowserEntry(workspaceBrowserFocusIndex);
      return;
    }
    addWorkspaceSelection(visibleEntries[workspaceBrowserFocusIndex].path);
  }
}

function selectWorkspaceBrowserEntry(index) {
  workspaceBrowserFocusIndex = index;
  renderWorkspaceBrowser();
}

function openWorkspaceBrowserEntry(index) {
  var entry = getVisibleWorkspaceBrowserEntries()[index];
  if (!entry) return;
  browseWorkspaces(entry.path);
}

function addCurrentWorkspaceFolder() {
  if (!workspaceBrowserPath) return;
  addWorkspaceSelection(workspaceBrowserPath);
}

function addWorkspaceSelection(path) {
  if (!path) return;
  if (!workspaceSelectionDraft.includes(path)) {
    workspaceSelectionDraft.push(path);
  }
  renderWorkspaceSelectionDraft();
}

function removeWorkspaceSelection(path) {
  workspaceSelectionDraft = workspaceSelectionDraft.filter(function(item) { return item !== path; });
  renderWorkspaceSelectionDraft();
}

function clearWorkspaceSelection() {
  workspaceSelectionDraft = [];
  renderWorkspaceSelectionDraft();
}

async function saveWorkspaceGroups() {
  await api(configUpdateRoute(), {
    method: 'PUT',
    body: JSON.stringify({ workspace_groups: workspaceGroups.slice() }),
  });
}

async function useWorkspaceGroup(index) {
  var group = workspaceGroups[index];
  if (!group || !Array.isArray(group.workspaces)) return;
  workspaceSelectionDraft = group.workspaces.slice();
  renderWorkspaceSelectionDraft();
  await applyWorkspaceSelection();
}

function editWorkspaceGroup(index) {
  var group = workspaceGroups[index];
  if (!group || !Array.isArray(group.workspaces)) return;
  workspaceSelectionDraft = group.workspaces.slice();
  showWorkspacePicker(false);
}

async function deleteWorkspaceGroup(index) {
  workspaceGroups = workspaceGroups.filter(function(_, i) { return i !== index; });
  renderWorkspaceGroups();
  try {
    await saveWorkspaceGroups();
  } catch (e) {
    showAlert('Failed to update workspace groups: ' + e.message);
    await fetchConfig();
  }
}

async function applyWorkspaceSelection() {
  var status = document.getElementById('workspace-apply-status');
  var settingsStatus = document.getElementById('settings-workspace-status');
  try {
    if (status) status.textContent = 'Switching...';
    if (settingsStatus) settingsStatus.textContent = 'Switching...';
    stopTasksStream();
    stopGitStream();
    resetBoardState();
    await api(workspaceUpdateRoute(), {
      method: 'PUT',
      body: JSON.stringify({ workspaces: workspaceSelectionDraft.slice() }),
    });
    activeWorkspaces = workspaceSelectionDraft.slice();
    workspacePickerRequired = activeWorkspaces.length === 0;
    await fetchConfig();
    if (status) status.textContent = 'Saved.';
    if (settingsStatus) settingsStatus.textContent = 'Updated.';
  } catch (e) {
    if (status) status.textContent = e.message;
    if (settingsStatus) settingsStatus.textContent = e.message;
    showAlert('Failed to switch workspaces: ' + e.message);
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
