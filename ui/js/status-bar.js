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

  var state;
  // tasksSource is the global SSE connection from state.js / api.js
  if (typeof tasksSource === "undefined" || tasksSource === null) {
    state = "closed";
  } else if (tasksSource.readyState === 0 /* CONNECTING */) {
    state = "reconnecting";
  } else if (tasksSource.readyState === 1 /* OPEN */) {
    state = "ok";
  } else {
    state = "closed";
  }

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
  var btn = document.getElementById("status-bar-terminal-btn");
  if (!panel) return;
  var isHidden = panel.classList.contains("hidden");
  panel.classList.toggle("hidden", !isHidden);
  if (btn) btn.setAttribute("aria-expanded", isHidden ? "true" : "false");
}

// Expose globally to fit the existing vanilla-JS pattern
window.initStatusBar = initStatusBar;
window.updateStatusBar = updateStatusBar;
window.toggleTerminalPanel = toggleTerminalPanel;

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", initStatusBar);
} else {
  initStatusBar();
}
