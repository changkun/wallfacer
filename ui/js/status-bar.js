// --- Status bar ---
// Thin always-visible footer that shows SSE connection health, active
// workspace, in-progress count, waiting count, and a stub terminal panel.

function initStatusBar() {
  // Keyboard shortcut: backtick toggles the terminal panel when no
  // input/textarea/select/contenteditable element is focused.
  document.addEventListener("keydown", function (e) {
    if (e.key !== "`") return;
    var tag = document.activeElement && document.activeElement.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
    var ce =
      document.activeElement &&
      document.activeElement.getAttribute("contenteditable");
    if (ce !== null && ce !== "false") return;
    e.preventDefault();
    toggleTerminalPanel();
  });

  updateStatusBar();
}

function updateStatusBar() {
  _updateConnDot();
  _updateCounts();
  _updateWorkspace();
}

function _updateConnDot() {
  var dot = document.getElementById("status-bar-conn-dot");
  var label = document.getElementById("status-bar-conn-label");
  if (!dot || !label) return;

  // _sseConnState is maintained by api.js for both leader and follower tabs.
  var state = typeof _sseConnState !== "undefined" ? _sseConnState : "closed";

  dot.className = "status-bar-conn-dot status-bar-conn-dot--" + state;

  var labelText =
    state === "ok"
      ? "Connected"
      : state === "reconnecting"
        ? "Reconnecting…"
        : "Disconnected";
  label.textContent = labelText;
  dot.setAttribute("aria-label", labelText);
}

function _updateCounts() {
  var inProgressEl = document.getElementById("status-bar-in-progress");
  var waitingEl = document.getElementById("status-bar-waiting");
  if (!inProgressEl || !waitingEl) return;

  var taskList =
    typeof tasks !== "undefined" && Array.isArray(tasks) ? tasks : [];
  var inProgressCount = 0;
  var waitingCount = 0;
  for (var i = 0; i < taskList.length; i++) {
    var s = taskList[i].status;
    if (s === "in_progress" || s === "committing") inProgressCount++;
    else if (s === "waiting" || s === "failed") waitingCount++;
  }

  inProgressEl.textContent = String(inProgressCount);
  waitingEl.textContent = String(waitingCount);
}

function _updateWorkspace() {
  var el = document.getElementById("status-bar-workspace");
  if (!el) return;

  var workspaces =
    typeof activeWorkspaces !== "undefined" && Array.isArray(activeWorkspaces)
      ? activeWorkspaces
      : [];
  var groups =
    typeof workspaceGroups !== "undefined" && Array.isArray(workspaceGroups)
      ? workspaceGroups
      : [];

  if (workspaces.length === 0) {
    el.textContent = "";
    el.style.display = "none";
    return;
  }

  var label = "";
  // Prefer the active group name if available
  if (groups.length > 0) {
    var activeGroup = groups.find(function (g) {
      return (
        Array.isArray(g.workspaces) &&
        g.workspaces.length === workspaces.length &&
        g.workspaces.every(function (w, i) {
          return w === workspaces[i];
        })
      );
    });
    if (activeGroup && activeGroup.name) {
      label = activeGroup.name;
    }
  }

  // Fall back to basename of the first workspace path
  if (!label && workspaces[0]) {
    var parts = workspaces[0].replace(/\/$/, "").split("/");
    label = parts[parts.length - 1] || workspaces[0];
  }

  el.textContent = label;
  el.style.display = label ? "" : "none";
}

function toggleTerminalPanel() {
  var panel = document.getElementById("status-bar-panel");
  var handle = document.getElementById("status-bar-panel-resize");
  var btn = document.getElementById("status-bar-terminal-btn");
  if (!panel) return;
  var isHidden = panel.classList.contains("hidden");
  panel.classList.toggle("hidden", !isHidden);
  if (handle) handle.classList.toggle("hidden", !isHidden);
  if (btn) btn.setAttribute("aria-expanded", isHidden ? "true" : "false");
}

// ---------------------------------------------------------------------------
// Resizable terminal panel
// ---------------------------------------------------------------------------
var _panelMinHeight = 80;
var _panelMaxHeight = 600;
var _panelStorageKey = "wallfacer-panel-height";

function _initPanelResize() {
  var handle = document.getElementById("status-bar-panel-resize");
  var panel = document.getElementById("status-bar-panel");
  if (!handle || !panel) return;

  // Restore persisted height
  var stored = localStorage.getItem(_panelStorageKey);
  if (stored) {
    var h = parseInt(stored, 10);
    if (h >= _panelMinHeight && h <= _panelMaxHeight) {
      panel.style.height = h + "px";
    }
  }

  var startY = 0;
  var startH = 0;

  function onMouseMove(e) {
    // Panel grows upward: mouse moving up (smaller clientY) = larger panel
    var delta = startY - e.clientY;
    var newH = Math.min(
      _panelMaxHeight,
      Math.max(_panelMinHeight, startH + delta),
    );
    panel.style.height = newH + "px";
  }

  function onMouseUp() {
    document.removeEventListener("mousemove", onMouseMove);
    document.removeEventListener("mouseup", onMouseUp);
    handle.classList.remove("status-bar-panel-resize--active");
    document.body.style.userSelect = "";
    document.body.style.cursor = "";
    // Persist
    localStorage.setItem(_panelStorageKey, parseInt(panel.style.height, 10));
  }

  handle.addEventListener("mousedown", function (e) {
    e.preventDefault();
    startY = e.clientY;
    startH = panel.offsetHeight;
    handle.classList.add("status-bar-panel-resize--active");
    document.body.style.userSelect = "none";
    document.body.style.cursor = "ns-resize";
    document.addEventListener("mousemove", onMouseMove);
    document.addEventListener("mouseup", onMouseUp);
  });
}

// Expose globally to fit the existing vanilla-JS pattern
window.initStatusBar = initStatusBar;
window.updateStatusBar = updateStatusBar;
window.toggleTerminalPanel = toggleTerminalPanel;

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", function () {
    initStatusBar();
    _initPanelResize();
  });
} else {
  initStatusBar();
  _initPanelResize();
}

// --- System status (About tab) ---

// loadSystemStatus fetches runtime debug info and renders it in the
// About tab's system status section.
function loadSystemStatus() {
  var container = document.getElementById("about-system-status");
  var content = document.getElementById("about-system-status-content");
  if (!container || !content) return;

  api(Routes.debug.runtime())
    .then(function (data) {
      var lines = [];

      // Goroutines and memory.
      lines.push(
        "<div>Goroutines: <strong>" +
          (data.go_goroutine_count || 0) +
          "</strong> &middot; Heap: <strong>" +
          formatBytes(data.go_heap_alloc_bytes || 0) +
          "</strong></div>",
      );

      // Active containers.
      lines.push(
        "<div>Active containers: <strong>" +
          (data.active_containers || 0) +
          "</strong></div>",
      );

      // Container circuit breaker.
      if (data.container_circuit) {
        var cc = data.container_circuit;
        var ccColor =
          cc.state === "closed"
            ? "var(--text-muted)"
            : "var(--accent)";
        lines.push(
          '<div>Circuit breaker: <strong style="color:' +
            ccColor +
            '">' +
            cc.state +
            "</strong>" +
            (cc.failures > 0
              ? " (" + cc.failures + " failures)"
              : "") +
            "</div>",
        );
      }

      // Worker stats.
      if (data.worker_stats) {
        var ws = data.worker_stats;
        var workerLine =
          "<div>Task workers: <strong>" +
          (ws.enabled ? "enabled" : "disabled") +
          "</strong> &middot; Active: <strong>" +
          (ws.active_workers || 0) +
          "</strong>";
        if (ws.creates > 0 || ws.execs > 0) {
          var total = (ws.execs || 0) + (ws.fallbacks || 0);
          var ratio = total > 0 ? Math.round(((ws.execs || 0) / total) * 100) : 0;
          workerLine +=
            " &middot; Creates: " +
            (ws.creates || 0) +
            " &middot; Execs: " +
            (ws.execs || 0) +
            (ws.fallbacks > 0 ? " &middot; Fallbacks: " + ws.fallbacks : "") +
            " &middot; Reuse: <strong>" +
            ratio +
            "%</strong>";
        }
        workerLine += "</div>";
        lines.push(workerLine);

        // Per-activity breakdown.
        if (ws.by_activity && Object.keys(ws.by_activity).length > 0) {
          var actParts = [];
          for (var act in ws.by_activity) {
            var a = ws.by_activity[act];
            actParts.push(
              act + ": " + (a.creates || 0) + "c/" + (a.execs || 0) + "e",
            );
          }
          lines.push(
            '<div style="padding-left:12px;">' + actParts.join(" &middot; ") + "</div>",
          );
        }
      }

      // Task states.
      if (data.task_states) {
        var ts = data.task_states;
        var parts = [];
        if (ts.in_progress) parts.push(ts.in_progress + " running");
        if (ts.waiting) parts.push(ts.waiting + " waiting");
        if (ts.backlog) parts.push(ts.backlog + " backlog");
        if (ts.done) parts.push(ts.done + " done");
        if (ts.failed) parts.push(ts.failed + " failed");
        if (parts.length > 0) {
          lines.push("<div>Tasks: " + parts.join(" &middot; ") + "</div>");
        }
      }

      content.innerHTML = lines.join("");
      container.style.display = "";
    })
    .catch(function () {
      container.style.display = "none";
    });
}

function formatBytes(bytes) {
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB";
  return (bytes / (1024 * 1024)).toFixed(1) + " MB";
}
