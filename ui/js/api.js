/** @typedef {import('./generated/types.js').Task} Task */
/** @typedef {import('./generated/types.js').TaskEvent} TaskEvent */
/** @typedef {import('./generated/types.js').TaskStatus} TaskStatus */
/** @typedef {import('./generated/types.js').TaskUsage} TaskUsage */
/** @typedef {import('./generated/types.js').TaskKind} TaskKind */
/** @typedef {import('./generated/types.js').EventType} EventType */
/** @typedef {import('./generated/types.js').RefinementSession} RefinementSession */
/** @typedef {import('./generated/types.js').ExecutionEnvironment} ExecutionEnvironment */

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
      // If the modal is open and this updated task is a dependency of the open
      // task, refresh the modal's dependency section immediately so status
      // badges update without waiting for the next full render cycle.
      if (typeof getOpenModalTaskId === 'function' && typeof renderModalDependencies === 'function') {
        var openId = getOpenModalTaskId();
        if (openId) {
          var openTask = findTaskById(openId);
          var openDeps = typeof getTaskDependencyIds === 'function'
            ? getTaskDependencyIds(openTask)
            : (openTask && Array.isArray(openTask.depends_on) ? openTask.depends_on : []);
          if (openTask && (openId === task.id || openDeps.indexOf(task.id) !== -1)) {
            renderModalDependencies(openTask);
          }
        }
      }
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
      const deleted = JSON.parse(e.data);
      const next = applyTaskDeleted({
        tasks: tasks,
        archivedTasks: archivedTasks,
        archivedPage: archivedPage,
      }, deleted);
      tasks = next.tasks;
      archivedTasks = next.archivedTasks;
      scheduleRender();
      if (typeof getOpenModalTaskId === 'function' && typeof renderModalDependencies === 'function') {
        var openId = getOpenModalTaskId();
        if (openId) {
          var openTask = findTaskById(openId);
          var openDeps = typeof getTaskDependencyIds === 'function'
            ? getTaskDependencyIds(openTask)
            : (openTask && Array.isArray(openTask.depends_on) ? openTask.depends_on : []);
          if (openTask && deleted && openDeps.indexOf(deleted.id) !== -1) {
            renderModalDependencies(openTask);
          }
        }
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
  // Capture the source reference at call time so finish() removes from the
  // correct object even if startTasksStream() replaces the module-level
  // tasksSource while this promise is in-flight.
  var capturedSource = tasksSource;
  if (!capturedSource || capturedSource.readyState === EventSource.CLOSED) {
    return fetchTasks();
  }
  return new Promise(function(resolve) {
    var done = false;
    var timer = null;

    function finish(useFetch) {
      if (done) return;
      done = true;
      if (timer !== null) { clearTimeout(timer); timer = null; }
      // Use capturedSource, not the module-level tasksSource.
      capturedSource.removeEventListener('task-updated', onUpdated);
      capturedSource.removeEventListener('task-deleted', onDeleted);
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

    capturedSource.addEventListener('task-updated', onUpdated);
    capturedSource.addEventListener('task-deleted', onDeleted);
    timer = setTimeout(function() { finish(true); }, ms);
  });
}

function findTaskById(taskId) {
  return tasks.find(function(t) { return t.id === taskId; }) ||
    archivedTasks.find(function(t) { return t.id === taskId; }) ||
    null;
}

/**
 * waitForTaskTitle keeps watching a task until it has a non-empty title, or
 * gives up after timeoutMs (default 30 000 ms). This makes manual task
 * creation resilient when the task-created delta arrives but the later
 * title-update delta is delayed or missed and we need a fetch fallback.
 */
function waitForTaskTitle(taskId, timeoutMs) {
  var deadline = Date.now() + (typeof timeoutMs === 'number' ? timeoutMs : 30000);

  function step() {
    var current = findTaskById(taskId);
    if (!current || current.title) {
      return Promise.resolve();
    }
    var remaining = deadline - Date.now();
    if (remaining <= 0) {
      return Promise.resolve();
    }
    return waitForTaskDelta(taskId, Math.min(remaining, 3000)).then(function() {
      return step();
    }, function() {
      return Promise.resolve();
    });
  }

  return step();
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

// --- Autopilot and automation toggles ---

function toggleAutomationMenu(event) {
  if (event && typeof event.stopPropagation === 'function') event.stopPropagation();
  var el = document.getElementById('automation-menu');
  if (!el) return;
  el.classList.toggle('hidden');
}

function hideAutomationMenu() {
  var el = document.getElementById('automation-menu');
  if (!el) return;
  el.classList.add('hidden');
}

document.addEventListener('click', function(e) {
  var wrap = document.querySelector('.automation-menu-wrap');
  if (wrap && !wrap.contains(e.target)) hideAutomationMenu();
});

function updateAutomationActiveCount() {
  var ids = ['ideation-header-toggle', 'autorefine-toggle', 'autopilot-toggle', 'autosync-toggle', 'autotest-toggle', 'autosubmit-toggle', 'autopush-toggle', 'dep-graph-toggle'];
  var count = 0;
  ids.forEach(function(id) {
    var cb = document.getElementById(id);
    if (cb && cb.checked) count++;
  });
  var badge = document.getElementById('automation-active-count');
  if (!badge) return;
  if (count > 0) {
    badge.textContent = count;
    badge.style.display = '';
  } else {
    badge.style.display = 'none';
  }
}

var _watcherFriendlyNames = {
  'auto-promote': 'Implement',
  'auto-retry':   'Retry',
  'auto-test':    'Test',
  'auto-submit':  'Submit',
  'auto-sync':    'Tip-sync',
  'auto-refine':  'Refine',
};

function updateWatcherHealth(entries) {
  var el = document.getElementById('watcher-health-section');
  if (!el) return;
  if (!Array.isArray(entries) || entries.length === 0) {
    el.innerHTML = '';
    return;
  }
  var tripped = entries.filter(function(e) { return !e.healthy; });
  var html = '<div class="watcher-health-header">Circuit Breakers</div><div class="watcher-health-list">';
  if (tripped.length === 0) {
    html += '<div class="watcher-health-row watcher-health-row--ok">'
      + '<span class="watcher-health-dot watcher-health-dot--ok"></span>'
      + '<span class="watcher-health-name">All healthy</span>'
      + '</div>';
  } else {
    tripped.forEach(function(e) {
      var label = _watcherFriendlyNames[e.name] || e.name;
      var parts = [];
      if (e.failures) parts.push(e.failures + (e.failures === 1 ? ' failure' : ' failures'));
      if (e.retry_at) {
        var retryMs = new Date(e.retry_at) - Date.now();
        if (retryMs > 0) {
          var s = Math.ceil(retryMs / 1000);
          parts.push('retry in ' + (s < 60 ? s + 's' : Math.ceil(s / 60) + 'm'));
        }
      }
      var titleAttr = e.last_reason ? ' title="' + escapeHtml(e.last_reason) + '"' : '';
      html += '<div class="watcher-health-row watcher-health-row--tripped"' + titleAttr + '>'
        + '<span class="watcher-health-dot watcher-health-dot--tripped"></span>'
        + '<span class="watcher-health-name">' + escapeHtml(label) + '</span>'
        + (parts.length ? '<span class="watcher-health-detail">' + escapeHtml(parts.join('; ')) + '</span>' : '')
        + '</div>';
    });
  }
  html += '</div>';
  el.innerHTML = html;
}

async function toggleAutopilot() {
  var toggle = document.getElementById('autopilot-toggle');
  var enabled = toggle ? toggle.checked : !autopilot;
  try {
    var res = await api(Routes.config.update(), { method: 'PUT', body: JSON.stringify({ autopilot: enabled }) });
    autopilot = !!res.autopilot;
    if (toggle) toggle.checked = autopilot;
    updateAutomationActiveCount();
  } catch (e) {
    showAlert('Error toggling autopilot: ' + e.message);
    if (toggle) toggle.checked = autopilot;
  }
}

async function toggleAutotest() {
  var toggle = document.getElementById('autotest-toggle');
  var enabled = toggle ? toggle.checked : !autotest;
  try {
    var res = await api(Routes.config.update(), { method: 'PUT', body: JSON.stringify({ autotest: enabled }) });
    autotest = !!res.autotest;
    if (toggle) toggle.checked = autotest;
    updateAutomationActiveCount();
  } catch (e) {
    showAlert('Error toggling auto-test: ' + e.message);
    if (toggle) toggle.checked = autotest;
  }
}

async function toggleAutosubmit() {
  var toggle = document.getElementById('autosubmit-toggle');
  var enabled = toggle ? toggle.checked : !autosubmit;
  try {
    var res = await api(Routes.config.update(), { method: 'PUT', body: JSON.stringify({ autosubmit: enabled }) });
    autosubmit = !!res.autosubmit;
    if (toggle) toggle.checked = autosubmit;
    updateAutomationActiveCount();
  } catch (e) {
    showAlert('Error toggling auto-submit: ' + e.message);
    if (toggle) toggle.checked = autosubmit;
  }
}

async function toggleAutorefine() {
  var toggle = document.getElementById('autorefine-toggle');
  var enabled = toggle ? toggle.checked : !autorefine;
  try {
    var res = await api(Routes.config.update(), { method: 'PUT', body: JSON.stringify({ autorefine: enabled }) });
    autorefine = !!res.autorefine;
    if (toggle) toggle.checked = autorefine;
    updateAutomationActiveCount();
  } catch (e) {
    showAlert('Error toggling auto-refine: ' + e.message);
    if (toggle) toggle.checked = autorefine;
  }
}

async function toggleAutosync() {
  var toggle = document.getElementById('autosync-toggle');
  var enabled = toggle ? toggle.checked : !autosync;
  try {
    var res = await api(Routes.config.update(), { method: 'PUT', body: JSON.stringify({ autosync: enabled }) });
    autosync = !!res.autosync;
    if (toggle) toggle.checked = autosync;
    updateAutomationActiveCount();
  } catch (e) {
    showAlert('Error toggling auto-sync: ' + e.message);
    if (toggle) toggle.checked = autosync;
  }
}

async function toggleAutopush() {
  var toggle = document.getElementById('autopush-toggle');
  var enabled = toggle ? toggle.checked : !autopush;
  try {
    var res = await api(Routes.config.update(), { method: 'PUT', body: JSON.stringify({ autopush: enabled }) });
    autopush = !!res.autopush;
    if (toggle) toggle.checked = autopush;
    updateAutomationActiveCount();
  } catch (e) {
    showAlert('Error toggling auto-push: ' + e.message);
    if (toggle) toggle.checked = autopush;
  }
}
