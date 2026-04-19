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

  // Handle plan deep-links: #plan/<path> (and legacy #spec/<path>).
  var specMatch = location.hash.match(/^#(?:plan|spec)\/(.+)$/);
  if (specMatch) {
    var specPath = decodeURIComponent(specMatch[1]);
    switchMode("spec");
    if (activeWorkspaces && activeWorkspaces.length > 0) {
      focusSpec(specPath, activeWorkspaces[0]);
    }
    return;
  }

  const match = location.hash.match(/^#([0-9a-f-]{36})(?:\/([\w-]+))?$/);
  if (!match) return;
  const taskId = match[1];
  const tabName = match[2] || null;
  const task =
    tasks.find((t) => t.id === taskId) ||
    archivedTasks.find((t) => t.id === taskId);
  if (!task) return;
  openModal(taskId).then(function () {
    if (tabName) {
      const mainTabs = [
        "spec",
        "activity",
        "changes",
        "flamegraph",
        "timeline",
        "events",
      ];
      const rightTabs = [
        "implementation",
        "testing",
        "changes",
        "spans",
        "timeline",
      ];
      const leftTabs = ["implementation", "testing"];
      if (mainTabs.includes(tabName) && typeof setMainTab === "function") {
        setMainTab(tabName);
      } else if (rightTabs.includes(tabName)) {
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
  archivedPage = {
    loadState: "idle",
    hasMoreBefore: false,
    hasMoreAfter: false,
  };
  if (shouldRender) scheduleRender();
}

function trimArchivedWindow(direction) {
  const next = trimArchivedWindowState(
    {
      tasks: tasks,
      archivedTasks: archivedTasks,
      archivedPage: archivedPage,
    },
    direction,
    archivedTasksPageSize,
  );
  archivedTasks = next.archivedTasks;
  archivedPage = next.archivedPage;
}

async function loadArchivedTasksPage(direction) {
  if (!showArchived) return;
  const dir = direction || "initial";
  if (dir === "before") {
    if (
      !archivedPage.hasMoreBefore ||
      archivedPage.loadState !== "idle" ||
      archivedTasks.length === 0
    )
      return;
    archivedPage.loadState = "loading-before";
  } else if (dir === "after") {
    if (
      !archivedPage.hasMoreAfter ||
      archivedPage.loadState !== "idle" ||
      archivedTasks.length === 0
    )
      return;
    archivedPage.loadState = "loading-after";
  } else {
    // 'initial'
    if (archivedPage.loadState !== "idle") return;
  }

  const pageSize = Math.max(1, archivedTasksPageSize || 20);
  let url =
    Routes.tasks.list() +
    "?include_archived=true&archived_page_size=" +
    encodeURIComponent(pageSize);
  if (dir === "before" && archivedTasks.length > 0) {
    url +=
      "&archived_before=" +
      encodeURIComponent(archivedTasks[archivedTasks.length - 1].id);
  }
  if (dir === "after" && archivedTasks.length > 0) {
    url += "&archived_after=" + encodeURIComponent(archivedTasks[0].id);
  }

  try {
    const resp = await api(url);
    const next = mergeArchivedTasksPage(
      {
        tasks: tasks,
        archivedTasks: archivedTasks,
        archivedPage: archivedPage,
      },
      resp,
      dir,
      pageSize,
    );
    archivedTasks = next.archivedTasks;
    archivedPage = next.archivedPage;
    scheduleRender();
  } catch (e) {
    console.error("loadArchivedTasksPage:", e);
  } finally {
    archivedPage.loadState = "idle";
  }
}

function onDoneColumnScroll() {
  if (!showArchived) return;
  const col = document.getElementById("col-done");
  if (!col) return;
  const nearTop = col.scrollTop <= 80;
  const nearBottom = col.scrollTop + col.clientHeight >= col.scrollHeight - 160;
  if (nearBottom) {
    loadArchivedTasksPage("before");
    return;
  }
  if (nearTop) {
    loadArchivedTasksPage("after");
  }
}

function ensureArchivedScrollBinding() {
  if (archivedScrollHandlerBound) return;
  const col = document.getElementById("col-done");
  if (!col) return;
  col.addEventListener("scroll", onDoneColumnScroll);
  archivedScrollHandlerBound = true;
}

// --- Tasks SSE event handlers (shared between leader and follower paths) ---

function _handleTasksSnapshot(data, lastEventId) {
  tasksRetryDelay = 1000;
  if (lastEventId) lastTasksEventId = lastEventId;
  try {
    const next = applyTasksSnapshot(
      {
        tasks: tasks,
        archivedTasks: archivedTasks,
        archivedPage: archivedPage,
      },
      data,
    );
    tasks = next.tasks;
    if (showArchived) {
      loadArchivedTasksPage("initial");
    } else {
      resetArchivedWindow(false);
    }
    scheduleRender();
    notifyTaskChangeListeners();
    // Seed the Board unread-dot seen-set so existing tasks never trigger
    // the dot on cold open — only tasks that appear later count.
    if (typeof initBoardUnreadSeen === "function") {
      initBoardUnreadSeen(
        tasks.map(function (t) {
          return t.id;
        }),
      );
    }
    // Resolve the initial mode from saved preference + task count + the
    // workspaceIsNew flag before the hash handler runs. Hash deep-links
    // (#plan/<path>) still win because they call switchMode afterwards.
    if (typeof resolveInitialMode === "function") {
      resolveInitialMode(tasks.length);
    }
    _handleInitialHash();
  } catch (err) {
    console.error("tasks SSE snapshot parse error:", err);
  }
}

function _handleTaskUpdated(data, lastEventId) {
  tasksRetryDelay = 1000;
  if (lastEventId) lastTasksEventId = lastEventId;
  try {
    const task = data;
    const reduced = applyTaskUpdated(
      {
        tasks: tasks,
        archivedTasks: archivedTasks,
        archivedPage: archivedPage,
      },
      task,
      {
        showArchived: showArchived,
        pageSize: archivedTasksPageSize,
      },
    );
    tasks = reduced.state.tasks;
    archivedTasks = reduced.state.archivedTasks;
    archivedPage = reduced.state.archivedPage;
    if (task.archived) {
      if (!showArchived) invalidateDiffBehindCounts(task.id);
      scheduleRender();
      notifyTaskChangeListeners();
      return;
    }
    if (
      typeof cardOversightCache !== "undefined" &&
      cardOversightCache &&
      typeof cardOversightCache.delete === "function"
    ) {
      cardOversightCache.delete(task.id);
    }
    if (reduced.previousTask && reduced.previousTask.status !== task.status) {
      announceBoardStatus(
        `Task "${getTaskAccessibleTitle(task)}" is now ${formatTaskStatusLabel(task.status)}`,
      );
    }
    // A task with no previousTask is newly created — surface the Board
    // unread dot if the user is not currently looking at the Board.
    if (!reduced.previousTask && typeof noteBoardNewTask === "function") {
      noteBoardNewTask(task.id);
    }
    invalidateDiffBehindCounts(task.id);
    scheduleRender();
    notifyTaskChangeListeners();
    if (
      typeof getOpenModalTaskId === "function" &&
      typeof renderModalDependencies === "function"
    ) {
      var openId = getOpenModalTaskId();
      if (openId) {
        var openTask = findTaskById(openId);
        var openDeps =
          typeof getTaskDependencyIds === "function"
            ? getTaskDependencyIds(openTask)
            : openTask && Array.isArray(openTask.depends_on)
              ? openTask.depends_on
              : [];
        if (
          openTask &&
          (openId === task.id || openDeps.indexOf(task.id) !== -1)
        ) {
          renderModalDependencies(openTask);
        }
      }
    }
  } catch (err) {
    console.error("tasks SSE task-updated parse error:", err);
  }
}

function _handleTaskDeleted(data, lastEventId) {
  tasksRetryDelay = 1000;
  if (lastEventId) lastTasksEventId = lastEventId;
  try {
    const deleted = data;
    const next = applyTaskDeleted(
      {
        tasks: tasks,
        archivedTasks: archivedTasks,
        archivedPage: archivedPage,
      },
      deleted,
    );
    tasks = next.tasks;
    archivedTasks = next.archivedTasks;
    scheduleRender();
    notifyTaskChangeListeners();
    if (
      typeof getOpenModalTaskId === "function" &&
      typeof renderModalDependencies === "function"
    ) {
      var openId = getOpenModalTaskId();
      if (openId) {
        var openTask = findTaskById(openId);
        var openDeps =
          typeof getTaskDependencyIds === "function"
            ? getTaskDependencyIds(openTask)
            : openTask && Array.isArray(openTask.depends_on)
              ? openTask.depends_on
              : [];
        if (openTask && deleted && openDeps.indexOf(deleted.id) !== -1) {
          renderModalDependencies(openTask);
        }
      }
    }
  } catch (err) {
    console.error("tasks SSE task-deleted parse error:", err);
  }
}

function startTasksStream() {
  if (!activeWorkspaces || activeWorkspaces.length === 0) {
    return;
  }
  if (tasksSource) tasksSource.close();
  ensureArchivedScrollBinding();

  // Follower tab: receive task events via BroadcastChannel relay instead of
  // opening a real SSE connection. Seed initial state with an HTTP fetch.
  if (!_sseIsLeader()) {
    tasksSource = null;
    _sseConnState = "reconnecting";
    _sseOnFollowerEvent("tasks-snapshot", function (data, id) {
      _sseConnState = "ok";
      _handleTasksSnapshot(data, id);
    });
    _sseOnFollowerEvent("tasks-updated", function (data, id) {
      _sseConnState = "ok";
      _handleTaskUpdated(data, id);
    });
    _sseOnFollowerEvent("tasks-deleted", function (data, id) {
      _sseConnState = "ok";
      _handleTaskDeleted(data, id);
    });
    fetchTasks().then(function () {
      _sseConnState = "ok";
    });
    return;
  }

  // Leader tab: open real EventSource and relay events to followers.
  const url = buildTasksStreamUrl(Routes.tasks.stream(), lastTasksEventId);
  tasksSource = new EventSource(url);
  _sseConnState = "reconnecting";
  _lastSSEEventTime = Date.now();

  tasksSource.addEventListener("open", function () {
    _sseConnState = "ok";
    if (typeof updateStatusBar === "function") updateStatusBar();
  });

  tasksSource.addEventListener("snapshot", function (e) {
    _sseConnState = "ok";
    _lastSSEEventTime = Date.now();
    var data = JSON.parse(e.data);
    _handleTasksSnapshot(data, e.lastEventId);
    _sseRelay("tasks-snapshot", data, e.lastEventId);
  });

  tasksSource.addEventListener("task-updated", function (e) {
    _sseConnState = "ok";
    _lastSSEEventTime = Date.now();
    var data = JSON.parse(e.data);
    _handleTaskUpdated(data, e.lastEventId);
    _sseRelay("tasks-updated", data, e.lastEventId);
  });

  tasksSource.addEventListener("task-deleted", function (e) {
    _sseConnState = "ok";
    _lastSSEEventTime = Date.now();
    var data = JSON.parse(e.data);
    _handleTaskDeleted(data, e.lastEventId);
    _sseRelay("tasks-deleted", data, e.lastEventId);
  });

  tasksSource.addEventListener("active_groups", function (e) {
    try {
      activeGroups = JSON.parse(e.data);
      if (typeof updateWorkspaceGroupBadges === "function")
        updateWorkspaceGroupBadges();
    } catch (err) {
      console.error("active_groups SSE parse error:", err);
    }
  });

  tasksSource.addEventListener("heartbeat", function () {
    _sseConnState = "ok";
    _lastSSEEventTime = Date.now();
  });

  tasksSource.onerror = function () {
    if (tasksSource.readyState === EventSource.CLOSED) {
      _sseConnState = "closed";
      tasksSource = null;
      if (typeof updateStatusBar === "function") updateStatusBar();
      var jittered = tasksRetryDelay * (1 + Math.random()); // uniform [base, 2×base]
      setTimeout(startTasksStream, jittered);
      tasksRetryDelay = Math.min(tasksRetryDelay * 2, 30000);
    } else {
      _sseConnState = "reconnecting";
      if (typeof updateStatusBar === "function") updateStatusBar();
    }
  };
}

function stopTasksStream() {
  if (tasksSource) tasksSource.close();
  tasksSource = null;
  _sseConnState = "closed";
  _lastSSEEventTime = 0;
}

function stopGitStream() {
  if (gitStatusHandle) {
    gitStatusHandle.stop();
    gitStatusHandle = null;
  }
}

function resetBoardState() {
  tasks = [];
  archivedTasks = [];
  archivedPage = {
    loadState: "idle",
    hasMoreBefore: false,
    hasMoreAfter: false,
  };
  gitStatuses = [];
  lastTasksEventId = null;
  rawLogBuffer = "";
  testRawLogBuffer = "";
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

// --- Visibility change fallback ---
// When the tab returns to the foreground, fetch the latest task list so that
// any SSE events missed while the tab was hidden (or due to a stale
// connection) are picked up immediately.
document.addEventListener("visibilitychange", function () {
  if (
    document.visibilityState === "visible" &&
    activeWorkspaces &&
    activeWorkspaces.length > 0
  ) {
    fetchTasks();
  }
});

// --- SSE heartbeat staleness detection ---
// Track when the last SSE event (data or heartbeat) was received. The server
// sends heartbeat events every 15 s. If nothing arrives for >35 s the
// connection is likely dead — fetch the full task list so the UI recovers
// without waiting for the browser's slow TCP timeout detection.
var _lastSSEEventTime = 0;
var _SSE_STALE_THRESHOLD_MS = 35000; // ~2× server heartbeat interval (15 s)
if (typeof setInterval === "function") {
  setInterval(function () {
    if (
      !_lastSSEEventTime ||
      !activeWorkspaces ||
      activeWorkspaces.length === 0
    )
      return;
    if (Date.now() - _lastSSEEventTime > _SSE_STALE_THRESHOLD_MS) {
      fetchTasks();
      // Force-restart the stream so a fresh connection is established.
      restartActiveStreams();
    }
  }, 10000);
}

/**
 * Fetches the current non-archived task list from the server.
 * @returns {Promise<Array.<Task>>}
 */
async function fetchTasks() {
  ensureArchivedScrollBinding();
  tasks = await api(Routes.tasks.list());
  tasks = tasks.filter(function (t) {
    return !t.archived;
  });
  if (showArchived) {
    await loadArchivedTasksPage("initial");
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
  var ms = typeof timeoutMs === "number" ? timeoutMs : 2000;
  // Capture the source reference at call time so finish() removes from the
  // correct object even if startTasksStream() replaces the module-level
  // tasksSource while this promise is in-flight.
  var capturedSource = tasksSource;
  if (!capturedSource || capturedSource.readyState === EventSource.CLOSED) {
    return fetchTasks();
  }
  return new Promise(function (resolve) {
    var done = false;
    var timer = null;

    function finish(useFetch) {
      if (done) return;
      done = true;
      if (timer !== null) {
        clearTimeout(timer);
        timer = null;
      }
      // Use capturedSource, not the module-level tasksSource.
      capturedSource.removeEventListener("task-updated", onUpdated);
      capturedSource.removeEventListener("task-deleted", onDeleted);
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

    capturedSource.addEventListener("task-updated", onUpdated);
    capturedSource.addEventListener("task-deleted", onDeleted);
    timer = setTimeout(function () {
      finish(true);
    }, ms);
  });
}

function findTaskById(taskId) {
  return (
    tasks.find(function (t) {
      return t.id === taskId;
    }) ||
    archivedTasks.find(function (t) {
      return t.id === taskId;
    }) ||
    null
  );
}

/**
 * waitForTaskTitle keeps watching a task until it has a non-empty title, or
 * gives up after timeoutMs (default 30 000 ms). This makes manual task
 * creation resilient when the task-created delta arrives but the later
 * title-update delta is delayed or missed and we need a fetch fallback.
 */
function waitForTaskTitle(taskId, timeoutMs) {
  var deadline =
    Date.now() + (typeof timeoutMs === "number" ? timeoutMs : 30000);

  function step() {
    var current = findTaskById(taskId);
    if (!current || current.title) {
      return Promise.resolve();
    }
    var remaining = deadline - Date.now();
    if (remaining <= 0) {
      return Promise.resolve();
    }
    return waitForTaskDelta(taskId, Math.min(remaining, 3000)).then(
      function () {
        // When the SSE stream is absent (follower tab or pre-election),
        // waitForTaskDelta falls back to fetchTasks() and resolves instantly.
        // Add a minimum 1 s delay to prevent a tight fetch loop.
        if (!tasksSource || tasksSource.readyState === EventSource.CLOSED) {
          return new Promise(function (resolve) {
            setTimeout(function () {
              resolve(step());
            }, 1000);
          });
        }
        return step();
      },
      function () {
        return Promise.resolve();
      },
    );
  }

  return step();
}

function toggleShowArchived() {
  showArchived = document.getElementById("show-archived-toggle").checked;
  localStorage.setItem(
    "wallfacer-show-archived",
    showArchived ? "true" : "false",
  );
  if (showArchived) {
    loadArchivedTasksPage("initial");
  } else {
    resetArchivedWindow(true);
  }
  startTasksStream();
}

// --- Autopilot and automation toggles ---

function toggleAutomationMenu(event) {
  if (event && typeof event.stopPropagation === "function")
    event.stopPropagation();
  var el = document.getElementById("automation-menu");
  if (!el) return;
  el.classList.toggle("hidden");
}

function hideAutomationMenu() {
  var el = document.getElementById("automation-menu");
  if (!el) return;
  el.classList.add("hidden");
}

document.addEventListener("click", function (e) {
  var wrap = document.querySelector(".automation-menu-wrap");
  if (wrap && !wrap.contains(e.target)) hideAutomationMenu();
});

function updateAutomationActiveCount() {
  var ids = [
    "ideation-header-toggle",
    "autorefine-toggle",
    "autopilot-toggle",
    "autosync-toggle",
    "autotest-toggle",
    "autosubmit-toggle",
    "autopush-toggle",
  ];
  var count = 0;
  ids.forEach(function (id) {
    var cb = document.getElementById(id);
    if (cb && cb.checked) count++;
  });
  var badge = document.getElementById("automation-active-count");
  if (!badge) return;
  if (count > 0) {
    badge.textContent = count;
    badge.style.display = "";
  } else {
    badge.style.display = "none";
  }
}

var _watcherFriendlyNames = {
  "auto-promote": "Implement",
  "auto-retry": "Retry",
  "auto-test": "Test",
  "auto-submit": "Submit",
  "auto-sync": "Catch Up",
  "auto-refine": "Refine",
};

function updateWatcherHealth(entries) {
  var el = document.getElementById("watcher-health-section");
  if (!el) return;
  if (!Array.isArray(entries) || entries.length === 0) {
    el.innerHTML = "";
    return;
  }
  var tripped = entries.filter(function (e) {
    return !e.healthy;
  });
  var html =
    '<div class="watcher-health-header">Circuit Breakers</div><div class="watcher-health-list">';
  if (tripped.length === 0) {
    html +=
      '<div class="watcher-health-row watcher-health-row--ok">' +
      '<span class="watcher-health-dot watcher-health-dot--ok"></span>' +
      '<span class="watcher-health-name">All healthy</span>' +
      "</div>";
  } else {
    tripped.forEach(function (e) {
      var label = _watcherFriendlyNames[e.name] || e.name;
      var parts = [];
      if (e.failures)
        parts.push(e.failures + (e.failures === 1 ? " failure" : " failures"));
      if (e.retry_at) {
        var retryMs = new Date(e.retry_at) - Date.now();
        if (retryMs > 0) {
          var s = Math.ceil(retryMs / 1000);
          parts.push(
            "retry in " + (s < 60 ? s + "s" : Math.ceil(s / 60) + "m"),
          );
        }
      }
      var titleAttr = e.last_reason
        ? ' title="' + escapeHtml(e.last_reason) + '"'
        : "";
      html +=
        '<div class="watcher-health-row watcher-health-row--tripped"' +
        titleAttr +
        ">" +
        '<span class="watcher-health-dot watcher-health-dot--tripped"></span>' +
        '<span class="watcher-health-name">' +
        escapeHtml(label) +
        "</span>" +
        (parts.length
          ? '<span class="watcher-health-detail">' +
            escapeHtml(parts.join("; ")) +
            "</span>"
          : "") +
        "</div>";
    });
  }
  html += "</div>";
  el.innerHTML = html;
}

var toggleAutopilot = createConfigToggle({
  elementId: "autopilot-toggle",
  configKey: "autopilot",
  getState: function () {
    return autopilot;
  },
  setState: function (v) {
    autopilot = v;
  },
  label: "autopilot",
  onUpdate: updateAutomationActiveCount,
});

var toggleAutotest = createConfigToggle({
  elementId: "autotest-toggle",
  configKey: "autotest",
  getState: function () {
    return autotest;
  },
  setState: function (v) {
    autotest = v;
  },
  label: "auto-test",
  onUpdate: updateAutomationActiveCount,
});

var toggleAutosubmit = createConfigToggle({
  elementId: "autosubmit-toggle",
  configKey: "autosubmit",
  getState: function () {
    return autosubmit;
  },
  setState: function (v) {
    autosubmit = v;
  },
  label: "auto-submit",
  onUpdate: updateAutomationActiveCount,
});

var toggleAutorefine = createConfigToggle({
  elementId: "autorefine-toggle",
  configKey: "autorefine",
  getState: function () {
    return autorefine;
  },
  setState: function (v) {
    autorefine = v;
  },
  label: "auto-refine",
  onUpdate: updateAutomationActiveCount,
});

var toggleAutosync = createConfigToggle({
  elementId: "autosync-toggle",
  configKey: "autosync",
  getState: function () {
    return autosync;
  },
  setState: function (v) {
    autosync = v;
  },
  label: "auto-sync",
  onUpdate: updateAutomationActiveCount,
});

var toggleAutopush = createConfigToggle({
  elementId: "autopush-toggle",
  configKey: "autopush",
  getState: function () {
    return autopush;
  },
  setState: function (v) {
    autopush = v;
  },
  label: "auto-push",
  onUpdate: updateAutomationActiveCount,
});
