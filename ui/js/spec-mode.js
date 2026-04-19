// --- Spec mode state and switching ---

// Internal mode values: "board" | "spec" | "depgraph" | "docs".
// Saved localStorage values: "board" | "plan" (see _persistSavedMode).
// "spec" and "plan" refer to the same mode — "spec" is the long-standing
// internal identifier, "plan" is the user-facing label introduced by the
// chat-first-mode rename.
var _validModes = {
  board: true,
  spec: true,
  depgraph: true,
  agents: true,
  flows: true,
  docs: true,
};

// resolveDefaultMode chooses the initial mode at app open. Priority:
//   1. Brand-new workspace → "plan"
//   2. Valid saved preference ("board" or "plan") → saved
//   3. Any task exists → "board"
//   4. Otherwise → "plan" (chat-first default)
function resolveDefaultMode(opts) {
  opts = opts || {};
  if (opts.workspaceIsNew) return "plan";
  if (opts.savedMode === "board" || opts.savedMode === "plan")
    return opts.savedMode;
  var taskCount = typeof opts.taskCount === "number" ? opts.taskCount : 0;
  if (taskCount > 0) return "board";
  return "plan";
}

function _readSavedMode() {
  if (typeof localStorage === "undefined") return null;
  var v = localStorage.getItem("wallfacer-mode");
  if (v === "board" || v === "plan") return v;
  // Pre-rename installs persisted "spec" as the saved mode. Migrate it
  // to "plan" on first read so the user's chat-first preference survives
  // the rename instead of silently falling back to Board.
  if (v === "spec") {
    localStorage.setItem("wallfacer-mode", "plan");
    return "plan";
  }
  return null;
}

function _persistSavedMode(internalMode) {
  // Only "board" and "spec" (saved as "plan") are remembered; other modes
  // such as "docs" leave the saved preference untouched.
  if (typeof localStorage === "undefined") return;
  if (internalMode === "board") {
    localStorage.setItem("wallfacer-mode", "board");
  } else if (internalMode === "spec") {
    localStorage.setItem("wallfacer-mode", "plan");
  }
}

function _modeToInternal(publicMode) {
  return publicMode === "plan" ? "spec" : publicMode;
}

// Initial currentMode honours any previously-persisted preference; if none
// is stored the provisional default is "board". The real initial mode is
// resolved in resolveInitialMode() once the first task snapshot arrives.
var _initialSavedMode = _readSavedMode();
var currentMode = _initialSavedMode
  ? _modeToInternal(_initialSavedMode)
  : "board";

// Session-only flag set when the user activates a new workspace group via
// PUT /api/workspaces. Causes the next resolveInitialMode call to force
// "plan" regardless of saved preference or task count. Cleared on the first
// substantive action (task created, chat message sent, spec focused).
var _workspaceIsNew = false;

// specModeState is the shared state object read by other modules
// (planning-chat.js, tests) to inspect Plan-mode state without touching
// private vars. The fields are kept in sync by focusSpec,
// focusRoadmapIndex, and the spec-tree load / SSE handlers.
//   - tree: current spec tree nodes array from /api/specs/tree
//           (empty array = no specs)
//   - index: Roadmap (specs/README.md) metadata or null
//   - focusedSpecPath: currently focused spec path or "" when none
var specModeState = {
  tree: [],
  index: null,
  focusedSpecPath: "",
};

// _applyLayout drives the Plan-mode layout state machine. Chat-first
// layout renders when the workspace has no specs and no Roadmap index;
// otherwise the three-pane explorer + focused-view + chat layout shows.
// The chosen layout is reflected on `#spec-mode-container` via
// `data-layout="chat-first" | "three-pane"`; CSS transitions in
// `ui/css/spec-mode.css` animate the switch.
function _applyLayout() {
  var container = document.getElementById("spec-mode-container");
  var explorerPanel = document.getElementById("explorer-panel");
  var treeNodes = (specModeState.tree && specModeState.tree.length) || 0;
  var hasIndex = !!specModeState.index;
  var layout = treeNodes === 0 && !hasIndex ? "chat-first" : "three-pane";
  if (container) {
    container.setAttribute("data-layout", layout);
    // Legacy class retained for backwards compatibility with CSS rules
    // and tests that predate the data-layout attribute.
    container.classList.toggle("spec-mode--chat-only", layout === "chat-first");
  }
  if (explorerPanel) {
    explorerPanel.style.display =
      layout === "three-pane" && getCurrentMode() === "spec" ? "" : "none";
  }
  // Chat-first is the only pane available; force it visible so a user
  // who previously hid the chat (C key in three-pane) doesn't land on
  // an empty Plan-mode page after switching to a workspace that has no
  // specs and no Roadmap.
  if (layout === "chat-first") {
    var chatStream = document.getElementById("spec-chat-stream");
    if (chatStream && chatStream.style.display === "none") {
      chatStream.style.display = "";
      _syncSpecChatToggle(true);
    }
  }
  _ensureChatMessagesObserver();
  _syncChatFirstEmptyHint();
}

// _chatMessagesObserver watches the chat message list so the empty-state
// hint is re-evaluated whenever planning-chat.js appends, clears, or
// rebuilds bubbles — avoids sprinkling _syncChatFirstEmptyHint calls
// across every mutation site.
var _chatMessagesObserver = null;

function _ensureChatMessagesObserver() {
  if (_chatMessagesObserver) return;
  if (typeof MutationObserver !== "function") return;
  var messages = document.getElementById("spec-chat-messages");
  if (!messages) return;
  _chatMessagesObserver = new MutationObserver(function () {
    _syncChatFirstEmptyHint();
  });
  _chatMessagesObserver.observe(messages, { childList: true });
}

// _syncChatFirstEmptyHint shows the "/create" hint and swaps the
// composer placeholder when Plan mode is in chat-first layout AND the
// active thread has no messages yet. Invoked from _applyLayout and via
// the MutationObserver above.
function _syncChatFirstEmptyHint() {
  var hint = document.getElementById("spec-chat-empty-hint");
  var input = document.getElementById("spec-chat-input");
  var messages = document.getElementById("spec-chat-messages");
  if (!hint || !input) return;
  var layout = getLayoutState();
  var hasMessages =
    !!messages && messages.querySelector(".planning-chat-bubble") !== null;
  var showHint = layout === "chat-first" && !hasMessages;
  hint.classList.toggle("spec-chat-empty-hint--visible", showHint);
  input.placeholder = showHint
    ? "Describe what you'd like to plan, or /create <title>..."
    : "Message...";
}

// getLayoutState returns the currently applied layout name. Exposed for
// keyboard-shortcut callers that need to short-circuit when the chat
// pane is the only visible pane.
function getLayoutState() {
  var container = document.getElementById("spec-mode-container");
  return (container && container.getAttribute("data-layout")) || "three-pane";
}

// --- Focused-view crossfade ---
//
// Whenever the focused entry changes (spec ↔ index or spec ↔ spec) the
// body element's content is swapped via a short CSS crossfade:
//   1. Fade the outgoing opacity to 0 over 140ms (accelerate curve).
//   2. 40ms into the fade, run the caller-provided replaceFn to swap
//      innerHTML (so the new content arrives while the container is
//      visually gone).
//   3. On the next frame, transition the container back to opacity 1
//      over 180ms (decelerate curve).
//
// An epoch counter absorbs click-spam: if a second crossfade starts
// while the first is still animating, the first's fade-in tick is
// discarded. The newer call re-drives the opacity so no frame is
// left stuck at 0.
var _focusedCrossfadeEpoch = 0;

// _formatSpecPath normalises a spec path for the breadcrumb element in the
// spec-focused-view header. The spec tree already emits paths prefixed with
// "specs/", but legacy callers sometimes pass workspace-relative paths
// without the prefix — prepend it so the displayed breadcrumb is always
// anchored at the specs/ root.
function _formatSpecPath(specPath) {
  if (!specPath) return "";
  return specPath.indexOf("specs/") === 0 ? specPath : "specs/" + specPath;
}

function _scheduleFocusedCrossfade(replaceFn) {
  var bodyInner = document.getElementById("spec-focused-body-inner");
  var reducedMotion = _prefersReducedMotion();
  if (!bodyInner || reducedMotion) {
    if (typeof replaceFn === "function") replaceFn();
    if (bodyInner) {
      bodyInner.style.opacity = "";
      bodyInner.style.transition = "";
    }
    return;
  }
  var myEpoch = ++_focusedCrossfadeEpoch;
  bodyInner.style.transition = "opacity 140ms cubic-bezier(0.3, 0, 0.8, 0.15)";
  bodyInner.style.opacity = "0";
  setTimeout(function () {
    if (myEpoch !== _focusedCrossfadeEpoch) return;
    if (typeof replaceFn === "function") replaceFn();
    // Next frame: switch to decelerate curve and fade back in.
    var schedule =
      typeof requestAnimationFrame === "function"
        ? requestAnimationFrame
        : function (fn) {
            return setTimeout(fn, 16);
          };
    schedule(function () {
      if (myEpoch !== _focusedCrossfadeEpoch) return;
      bodyInner.style.transition = "opacity 180ms cubic-bezier(0.2, 0, 0, 1)";
      bodyInner.style.opacity = "1";
    });
  }, 40);
}

function _prefersReducedMotion() {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function")
    return false;
  try {
    var mq = window.matchMedia("(prefers-reduced-motion: reduce)");
    return !!(mq && mq.matches);
  } catch (_e) {
    return false;
  }
}

// Guards auto mode resolution. Starts true at boot; flipped to false once
// resolveInitialMode() has run or once the user explicitly switches. Reset
// to true when the active workspace group changes.
var _resolutionPending = true;

function getCurrentMode() {
  return currentMode;
}

function setCurrentMode(mode) {
  currentMode = mode;
}

function markWorkspaceIsNew() {
  _workspaceIsNew = true;
  _resolutionPending = true;
}

function clearWorkspaceIsNew() {
  _workspaceIsNew = false;
}

// resolveInitialMode is called once after the first task snapshot. It picks
// the initial mode from saved preference, task count, and the
// workspaceIsNew flag, then switches without persisting.
function resolveInitialMode(taskCount) {
  if (!_resolutionPending) return;
  _resolutionPending = false;
  var publicMode = resolveDefaultMode({
    savedMode: _readSavedMode(),
    taskCount: taskCount,
    workspaceIsNew: _workspaceIsNew,
  });
  var target = _modeToInternal(publicMode);
  if (target !== currentMode) {
    switchMode(target);
  } else {
    // Even when already in the right mode, make sure the DOM reflects it
    // (module-load only set the variable; the DOM was not touched).
    _applyMode(target);
  }
}

// _applyMode updates the DOM to reflect the given mode without checking
// whether the mode has changed. Used both by switchMode (after the
// idempotency check) and by DOMContentLoaded (to restore persisted mode
// where the JS variable already matches but the DOM hasn't been touched).
function _applyMode(mode) {
  // Update sidebar navigation active states.
  var boardNav = document.getElementById("sidebar-nav-board");
  var specNav = document.getElementById("sidebar-nav-spec");
  var depgraphNav = document.getElementById("sidebar-nav-depgraph");
  var agentsNav = document.getElementById("sidebar-nav-agents");
  var flowsNav = document.getElementById("sidebar-nav-flows");
  var docsNav = document.getElementById("sidebar-nav-docs");
  var usageNav = document.getElementById("sidebar-nav-usage");
  if (boardNav) boardNav.classList.toggle("active", mode === "board");
  if (specNav) specNav.classList.toggle("active", mode === "spec");
  if (depgraphNav) depgraphNav.classList.toggle("active", mode === "depgraph");
  if (agentsNav) agentsNav.classList.toggle("active", mode === "agents");
  if (flowsNav) flowsNav.classList.toggle("active", mode === "flows");
  if (docsNav) docsNav.classList.toggle("active", mode === "docs");
  if (usageNav) usageNav.classList.toggle("active", mode === "analytics");

  // Toggle main content areas.
  var board = document.getElementById("board");
  var specView = document.getElementById("spec-mode-container");
  var depgraphView = document.getElementById("depgraph-mode-container");
  var agentsView = document.getElementById("agents-mode-container");
  var flowsView = document.getElementById("flows-mode-container");
  var docsView = document.getElementById("docs-mode-container");
  var analyticsView = document.getElementById("analytics-mode-container");
  if (board) board.style.display = mode === "board" ? "" : "none";
  if (specView) specView.style.display = mode === "spec" ? "" : "none";
  if (depgraphView)
    depgraphView.style.display = mode === "depgraph" ? "" : "none";
  if (agentsView) agentsView.style.display = mode === "agents" ? "" : "none";
  if (flowsView) flowsView.style.display = mode === "flows" ? "" : "none";
  if (docsView) docsView.style.display = mode === "docs" ? "" : "none";
  if (analyticsView)
    analyticsView.style.display = mode === "analytics" ? "" : "none";
  if (typeof document !== "undefined" && document.body) {
    document.body.classList.toggle("analytics-mode-on", mode === "analytics");
  }
  if (mode === "analytics" && typeof window.enterAnalyticsMode === "function") {
    window.enterAnalyticsMode();
  }

  // Load agents list when entering agents mode for the first time.
  if (mode === "agents" && typeof window.loadAgents === "function") {
    window.loadAgents();
  }
  // Load flows list when entering flows mode for the first time.
  if (mode === "flows" && typeof window.loadFlows === "function") {
    window.loadFlows();
  }

  // Toggle the dep-graph rendering flag so the existing panel renders into
  // #depgraph-mode-container while this mode is active. The panel is mounted
  // on first render (see depgraph.js getOrCreatePanel).
  if (typeof window !== "undefined") {
    var leavingDepgraph = window.depGraphEnabled && mode !== "depgraph";
    window.depGraphEnabled = mode === "depgraph";
    if (leavingDepgraph && typeof window._resetMapCentering === "function") {
      // Re-anchor the content on the next Map open — user was working in
      // a different mode, so it's fair to show them the graph's centroid
      // again when they come back.
      window._resetMapCentering();
    }
  }
  if (mode === "depgraph") {
    // Populate the spec tree so the unified renderer has both sides of the
    // graph. loadSpecTree is idempotent and calls _syncSpecModeState which
    // scheduleRender picks up on its next tick.
    if (typeof loadSpecTree === "function") {
      try {
        loadSpecTree();
      } catch (_e) {
        // Best-effort; dep graph falls back to task-only when tree is empty.
      }
    }
    // Seed the map search with whatever is already in the global
    // task-search input — users who searched on the board and then flipped
    // to Map expect the filter to carry over.
    if (typeof setMapSearch === "function") {
      var seedInput = document.getElementById("task-search");
      var seed = seedInput && seedInput.value ? seedInput.value : "";
      if (seed && seed.charAt(0) !== "@") setMapSearch(seed);
    }
    if (typeof scheduleRender === "function") scheduleRender();
    else if (typeof render === "function") render();
  }

  // Stop spec refresh poll and clear hash when leaving spec mode.
  if (mode !== "spec") {
    _stopSpecRefreshPoll();
    if (
      typeof location !== "undefined" &&
      location.hash &&
      (location.hash.indexOf("#plan/") === 0 ||
        location.hash.indexOf("#spec/") === 0)
    ) {
      history.replaceState(null, "", location.pathname);
    }
  }

  // Load docs content when entering docs mode for the first time.
  if (mode === "docs" && typeof _ensureDocsLoaded === "function") {
    _ensureDocsLoaded();
  }

  // Switch explorer root (no-op until spec-explorer is wired).
  if (typeof switchExplorerRoot === "function") {
    switchExplorerRoot(mode === "spec" ? "specs" : "workspace");
  }

  // In spec mode, hide explorer initially — it will be shown by
  // _updateSpecPaneVisibility() after the spec tree loads.
  // In other modes, always hide it.
  var explorerPanel = document.getElementById("explorer-panel");
  if (explorerPanel) {
    explorerPanel.style.display = "none";
  }

  // Hide workspace bar and content header in docs mode.
  var header =
    typeof document.querySelector === "function"
      ? document.querySelector(".app-header")
      : null;
  var gitBar = document.getElementById("workspace-git-bar");
  var fullPageMode = mode === "docs" || mode === "analytics";
  if (header) header.style.display = fullPageMode ? "none" : "";
  if (gitBar) gitBar.style.display = fullPageMode ? "none" : "";

  // Update search placeholder based on mode.
  var searchInput = document.getElementById("task-search");
  if (searchInput) {
    if (mode === "spec") {
      searchInput.placeholder = "Filter plans\u2026";
    } else if (mode === "depgraph") {
      searchInput.placeholder = "Filter map nodes\u2026";
    } else {
      searchInput.placeholder = "Filter tasks\u2026 or @search server";
    }
  }

  // Clear spec text filter when leaving spec mode.
  if (mode !== "spec" && typeof setSpecTextFilter === "function") {
    setSpecTextFilter("");
  }
  // Clear map search when leaving Map mode.
  if (mode !== "depgraph" && typeof setMapSearch === "function") {
    setMapSearch("");
  }
}

// --- Sidebar collapse/expand ---

function toggleSidebar() {
  var sidebar = document.getElementById("app-sidebar");
  if (!sidebar) return;
  var collapsed = sidebar.classList.toggle("collapsed");
  localStorage.setItem("wallfacer-sidebar-collapsed", collapsed ? "1" : "");
}

function expandSidebar() {
  var sidebar = document.getElementById("app-sidebar");
  if (!sidebar || !sidebar.classList.contains("collapsed")) return;
  toggleSidebar();
}

function _restoreSidebarState() {
  if (localStorage.getItem("wallfacer-sidebar-collapsed") === "1") {
    var sidebar = document.getElementById("app-sidebar");
    if (sidebar) sidebar.classList.add("collapsed");
  }
}

// _highlightTaskId holds a task ID to scroll to and highlight after
// switching to board mode from a focused spec. Cleared after use.
var _highlightTaskId = null;

// switchMode toggles between board, spec, and docs modes. Updates sidebar
// nav active states and swaps main content visibility. The saved
// preference is only updated when opts.persist is true — reserved for
// explicit user actions (sidebar nav click, keyboard shortcut).
function switchMode(mode, opts) {
  // An explicit user switch cancels any pending auto-resolution and
  // updates the persisted preference even when clicking the already-
  // active mode. Without this, clicking the provisional Board nav while
  // resolution is still pending would leave _resolutionPending=true and
  // the next task snapshot could flip back to Plan against the click.
  if (opts && opts.persist) {
    _resolutionPending = false;
    _persistSavedMode(mode);
  }
  if (mode === currentMode) return;

  // When leaving spec mode for board mode, capture the dispatched task ID
  // so we can highlight it on the board.
  if (currentMode === "spec" && mode === "board" && _focusedSpecContent) {
    var parsed = parseSpecFrontmatter(_focusedSpecContent);
    var dtid = parsed.frontmatter.dispatched_task_id;
    if (dtid && dtid !== "null") {
      _highlightTaskId = dtid;
    }
  }

  setCurrentMode(mode);
  _applyMode(mode);

  // Entering Board clears the sidebar unread dot — every visible task is
  // implicitly "seen" for the purpose of dismissing the bridge badge.
  if (mode === "board" && typeof clearBoardUnreadDot === "function") {
    clearBoardUnreadDot();
  }

  // After switching to board, highlight the task card from the focused spec.
  if (mode === "board" && _highlightTaskId) {
    _highlightBoardTask(_highlightTaskId);
    _highlightTaskId = null;
  }
}

// _highlightBoardTask finds a task card on the board and scrolls to it
// with a brief highlight animation.
function _highlightBoardTask(taskId) {
  var card =
    typeof document.querySelector === "function"
      ? document.querySelector('.card[data-task-id="' + taskId + '"]')
      : null;
  if (!card) return;

  card.scrollIntoView({ behavior: "smooth", block: "center" });
  card.classList.add("card-highlight");
  card.addEventListener(
    "animationend",
    function () {
      card.classList.remove("card-highlight");
    },
    { once: true },
  );
}

// --- Task-mode Plan navigation ---

// State for the currently focused task in Plan mode (set by openPlanForTask).
var _focusedTaskId = null;
var _focusedTaskTitle = null;
var _focusedTaskStatus = null;

// openPlanForTask opens Plan mode with the planning thread pinned to taskId.
// It reuses an existing non-archived task-mode thread for the task if one
// exists; otherwise it creates a new thread. Then it switches to Plan mode.
function openPlanForTask(taskId, title, status) {
  if (!taskId) return;
  _focusedTaskId = taskId;
  _focusedTaskTitle = title || "";
  _focusedTaskStatus = status || "";
  clearWorkspaceIsNew();

  // Fetch existing threads, find a non-archived task-mode thread for this task.
  api(Routes.planning.listThreads() + "?includeArchived=false")
    .then(function (res) {
      var threads = (res && res.threads) || [];
      var match = null;
      for (var i = 0; i < threads.length; i++) {
        var t = threads[i];
        if (t.mode === "task" && t.task_id === taskId && !t.archived) {
          match = t;
          break;
        }
      }
      if (match) {
        // Activate existing thread then reload.
        return fetch(
          Routes.planning.activateThread().replace("{id}", match.id),
          {
            method: "POST",
            headers: withBearerHeaders({ "Content-Type": "application/json" }),
          },
        ).then(function () {
          if (
            typeof PlanningChat !== "undefined" &&
            typeof PlanningChat.reload === "function"
          ) {
            PlanningChat.reload();
          }
        });
      }
      // No existing thread: create one pinned to this task.
      var name = "Task prompt: " + (_focusedTaskTitle || taskId);
      return api(Routes.planning.createThread(), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: name, focused_task: taskId }),
      }).then(function () {
        if (
          typeof PlanningChat !== "undefined" &&
          typeof PlanningChat.reload === "function"
        ) {
          PlanningChat.reload();
        }
      });
    })
    .catch(function (err) {
      console.error("openPlanForTask:", err);
    });

  // Update breadcrumb and switch to Plan mode.
  _updateTaskBreadcrumb();
  switchMode("spec");
}

// _updateTaskBreadcrumb sets the Plan view header breadcrumb elements for the
// currently focused task. Called by openPlanForTask after state is set.
function _updateTaskBreadcrumb() {
  var pathEl = document.getElementById("spec-focused-path");
  var titleEl = document.getElementById("spec-focused-title");
  var statusEl = document.getElementById("spec-focused-status");
  var kindEl = document.getElementById("spec-focused-kind");
  var effortEl = document.getElementById("spec-focused-effort");
  var metaEl = document.getElementById("spec-focused-meta");
  var bodyInner = document.getElementById("spec-focused-body-inner");

  if (pathEl)
    pathEl.textContent =
      "Task Prompt" +
      (_focusedTaskTitle ? " \u00b7 " + _focusedTaskTitle : "") +
      (_focusedTaskStatus ? " (" + _focusedTaskStatus + ")" : "");
  if (titleEl) titleEl.textContent = _focusedTaskTitle || "";
  if (statusEl) statusEl.textContent = "";
  if (kindEl) kindEl.textContent = "";
  if (effortEl) effortEl.textContent = "";
  if (metaEl) metaEl.innerHTML = "";
  if (bodyInner) bodyInner.innerHTML = "";
}

// --- Focused spec view ---

var _focusedSpecPath = null;
var _focusedSpecWorkspace = null;
var _focusedSpecContent = null;
var _specRefreshTimer = null;
// When true, the focused view shows the pinned Roadmap (specs/README.md)
// instead of a real spec — spec affordances (status chip, dispatch,
// archive, effort badge) are hidden via the .spec-focused-view--index
// class in ui/css/spec-mode.css.
var _focusedIsIndex = false;

// focusSpec loads and renders a spec file in the focused markdown view.
function focusSpec(specPath, workspace) {
  // Focusing a spec is a substantive action — clear the new-workspace
  // bias so the next auto-resolution respects saved preference + tasks.
  clearWorkspaceIsNew();
  // Leaving the Roadmap index: drop the .spec-focused-view--index marker.
  if (_focusedIsIndex) {
    _focusedIsIndex = false;
    var _focusedViewEl0 = document.getElementById("spec-focused-view");
    if (_focusedViewEl0) {
      _focusedViewEl0.classList.remove("spec-focused-view--index");
    }
  }
  _focusedSpecPath = specPath;
  _focusedSpecWorkspace = workspace;
  _focusedSpecContent = null; // reset so loading indicator shows
  specModeState.focusedSpecPath = specPath || "";

  // Show loading state in the focused view with a crossfade so the
  // old content dissolves rather than snapping.
  var titleEl = document.getElementById("spec-focused-title");
  if (titleEl) titleEl.textContent = specPath;
  var pathEl = document.getElementById("spec-focused-path");
  if (pathEl) pathEl.textContent = _formatSpecPath(specPath);
  _scheduleFocusedCrossfade(function () {
    // Guard against the fetch winning the race with the 40ms crossfade
    // delay: when _focusedSpecContent is already populated, the spec has
    // rendered and the loading placeholder would silently overwrite it.
    if (_focusedSpecContent !== null) return;
    var innerEl = document.getElementById("spec-focused-body-inner");
    if (innerEl) {
      innerEl.innerHTML = '<div class="spec-loading">Loading\u2026</div>';
    }
  });

  _loadAndRenderSpec();
  _startSpecRefreshPoll();
  // Update hash for deep-linking. Writes always use the #plan/ prefix;
  // the reader also accepts legacy #spec/ URLs for backward compatibility.
  history.replaceState(null, "", "#plan/" + encodeURIComponent(specPath));
}

function getFocusedSpecPath() {
  return _focusedSpecPath;
}

// isRoadmapFocused returns true when the focused-view currently shows
// the pinned Roadmap (specs/README.md) rather than a regular spec.
function isRoadmapFocused() {
  return _focusedIsIndex;
}

// focusRoadmapIndex loads specs/README.md into the focused-view and
// hides all spec affordances (status chip, dispatch, archive, effort
// badges, depends_on indicator). The pinned Roadmap entry in the
// explorer calls this instead of focusSpec(). `indexMeta` is the
// Index object from GET /api/specs/tree: `{path, workspace, title,
// modified}`. When indexMeta is null, the call is a no-op.
function focusRoadmapIndex(indexMeta) {
  if (!indexMeta || !indexMeta.path || !indexMeta.workspace) return;
  clearWorkspaceIsNew();
  _stopSpecRefreshPoll();
  _focusedIsIndex = true;
  _focusedSpecPath = indexMeta.path;
  _focusedSpecWorkspace = indexMeta.workspace;
  _focusedSpecContent = null;
  specModeState.focusedSpecPath = indexMeta.path;

  var focusedView = document.getElementById("spec-focused-view");
  if (focusedView) focusedView.classList.add("spec-focused-view--index");

  // Reset all per-spec header fields and hide buttons — the markdown
  // rendering path for regular specs touches these too, but for the
  // index we never rewrite them after the first clear.
  var titleEl = document.getElementById("spec-focused-title");
  if (titleEl) titleEl.textContent = "Roadmap";
  var pathEl = document.getElementById("spec-focused-path");
  if (pathEl) pathEl.textContent = "";
  var metaEl = document.getElementById("spec-focused-meta");
  if (metaEl) metaEl.textContent = "";
  var clearIds = [
    "spec-focused-status",
    "spec-focused-kind",
    "spec-focused-effort",
  ];
  for (var ci = 0; ci < clearIds.length; ci++) {
    var cel = document.getElementById(clearIds[ci]);
    if (cel) {
      cel.textContent = "";
      cel.className = cel.className.replace(/ spec-\S+/g, "");
    }
  }
  var hideIds = [
    "spec-dispatch-btn",
    "spec-summarize-btn",
    "spec-archive-btn",
    "spec-unarchive-btn",
    "spec-archived-banner",
  ];
  for (var hi = 0; hi < hideIds.length; hi++) {
    var hel = document.getElementById(hideIds[hi]);
    if (hel) hel.classList.add("hidden");
  }

  _scheduleFocusedCrossfade(function () {
    // See focusSpec for why this guard exists.
    if (_focusedSpecContent !== null) return;
    var bodyInnerInit = document.getElementById("spec-focused-body-inner");
    if (bodyInnerInit) {
      bodyInnerInit.innerHTML = '<div class="spec-loading">Loading\u2026</div>';
    }
  });
  var bodyInner = document.getElementById("spec-focused-body-inner");

  var absPath = indexMeta.workspace + "/" + indexMeta.path;
  var url =
    Routes.explorer.readFile() +
    "?path=" +
    encodeURIComponent(absPath) +
    "&workspace=" +
    encodeURIComponent(indexMeta.workspace);
  fetch(url, { headers: withBearerHeaders() })
    .then(function (res) {
      if (!res.ok) throw new Error("HTTP " + res.status);
      return res.text();
    })
    .then(function (text) {
      _focusedSpecContent = text;
      if (!bodyInner) return;
      bodyInner.innerHTML = renderMarkdown(text);
      // Roadmap's own H1 is redundant with the "Roadmap" title bar.
      var first = bodyInner.firstElementChild;
      if (first && first.tagName === "H1") first.remove();
      first = bodyInner.firstElementChild;
      if (first && first.tagName === "HR") first.remove();
      if (_mdRender && typeof _mdRender.enhanceMarkdown === "function") {
        _mdRender
          .enhanceMarkdown(bodyInner, {
            links: true,
            linkHandler: "spec",
            basePath: indexMeta.path,
            workspace: indexMeta.workspace,
          })
          .then(function () {
            var scrollEl = document.getElementById("spec-focused-body");
            var anchorEl = document.getElementById("spec-focused-view");
            if (
              scrollEl &&
              anchorEl &&
              typeof buildFloatingToc === "function"
            ) {
              buildFloatingToc(bodyInner, scrollEl, anchorEl, {
                headingSelector: "h1, h2, h3, h4",
                idPrefix: "spec-heading",
              });
            }
          });
      }
    })
    .catch(function (err) {
      console.error("roadmap load error:", err);
      if (bodyInner) {
        bodyInner.innerHTML =
          '<div class="spec-loading">Failed to load Roadmap</div>';
      }
    });
}

function _loadAndRenderSpec() {
  if (!_focusedSpecPath) return;

  // Always use the current active workspace so that a workspace-group
  // switch doesn't leave us requesting a stale, now-rejected workspace.
  var ws =
    typeof activeWorkspaces !== "undefined" && activeWorkspaces.length > 0
      ? activeWorkspaces[0]
      : _focusedSpecWorkspace;
  if (!ws) return;
  _focusedSpecWorkspace = ws;

  // The spec tree returns paths relative to the workspace root and
  // already includes the "specs/" prefix (e.g., "specs/local/foo.md").
  // The explorer file API expects an absolute path within the workspace,
  // so just concatenate workspace + path. (Earlier code added another
  // "/specs/" segment here, producing "/specs/specs/..." URLs that
  // 404'd from /api/explorer/file.)
  var absPath = ws + "/" + _focusedSpecPath;
  var url =
    Routes.explorer.readFile() +
    "?path=" +
    encodeURIComponent(absPath) +
    "&workspace=" +
    encodeURIComponent(ws);

  fetch(url, { headers: withBearerHeaders() })
    .then(function (res) {
      if (!res.ok) throw new Error("HTTP " + res.status);
      return res.text();
    })
    .then(function (text) {
      if (text === _focusedSpecContent) return;
      _focusedSpecContent = text;

      var parsed = parseSpecFrontmatter(text);
      var titleEl = document.getElementById("spec-focused-title");
      var statusEl = document.getElementById("spec-focused-status");
      var bodyEl = document.getElementById("spec-focused-body-inner");
      var dispatchBtn = document.getElementById("spec-dispatch-btn");

      if (titleEl)
        titleEl.textContent = parsed.frontmatter.title || _focusedSpecPath;
      var pathEl = document.getElementById("spec-focused-path");
      if (pathEl) pathEl.textContent = _formatSpecPath(_focusedSpecPath);
      if (statusEl) {
        var status = parsed.frontmatter.status || "";
        statusEl.textContent = status;
        statusEl.className = "spec-focused-view__status";
        if (status) statusEl.classList.add("spec-status--" + status);
      }

      // Determine if this is a design spec (has children) or implementation spec (leaf).
      var kindEl = document.getElementById("spec-focused-kind");
      if (kindEl) {
        var isLeaf = true;
        if (
          typeof _specTreeData !== "undefined" &&
          _specTreeData &&
          _specTreeData.nodes
        ) {
          var treeNode = _specTreeData.nodes.find(function (n) {
            return n.path === _focusedSpecPath;
          });
          if (treeNode) isLeaf = treeNode.is_leaf;
        }
        kindEl.textContent = isLeaf ? "implementation" : "design";
        kindEl.className =
          "spec-focused-view__kind spec-kind--" + (isLeaf ? "impl" : "design");
      }

      // Show effort badge.
      var effortEl = document.getElementById("spec-focused-effort");
      if (effortEl) {
        effortEl.textContent = parsed.frontmatter.effort || "";
      }

      // Show metadata bar: author, dates, depends_on, affects.
      var metaEl = document.getElementById("spec-focused-meta");
      if (metaEl) {
        var parts = [];
        if (parsed.frontmatter.author)
          parts.push("Author: " + parsed.frontmatter.author);
        if (parsed.frontmatter.created)
          parts.push("Created: " + parsed.frontmatter.created);
        if (parsed.frontmatter.updated)
          parts.push("Updated: " + parsed.frontmatter.updated);
        metaEl.textContent = parts.join(" \u00B7 ");
      }

      if (bodyEl) {
        bodyEl.innerHTML = renderMarkdown(parsed.body);
        // Remove the leading h1 and hr — they duplicate the title bar.
        var firstChild = bodyEl.firstElementChild;
        if (firstChild && firstChild.tagName === "H1") {
          firstChild.remove();
        }
        // Remove leading <hr> after the title (from "---" in markdown).
        firstChild = bodyEl.firstElementChild;
        if (firstChild && firstChild.tagName === "HR") {
          firstChild.remove();
        }
        // Post-process: mermaid diagrams and spec link navigation.
        _mdRender
          .enhanceMarkdown(bodyEl, {
            links: true,
            linkHandler: "spec",
            basePath: _focusedSpecPath || "",
            workspace: _focusedSpecWorkspace,
          })
          .then(function () {
            var scrollEl = document.getElementById("spec-focused-body");
            var anchorEl = document.getElementById("spec-focused-view");
            if (scrollEl && anchorEl) {
              buildFloatingToc(bodyEl, scrollEl, anchorEl, {
                headingSelector: "h1, h2, h3, h4",
                idPrefix: "spec-heading",
              });
            }
          });
      }

      var isArchived = parsed.frontmatter.status === "archived";

      // Archived banner at the top of the body (read-only signal).
      var bannerEl = document.getElementById("spec-archived-banner");
      if (bannerEl) {
        bannerEl.classList.toggle("hidden", !isArchived);
      }

      // Show dispatch button only for validated leaf specs (implementation specs).
      // Non-leaf (design) specs must be broken down first.
      if (dispatchBtn) {
        var isValidated = parsed.frontmatter.status === "validated";
        var specIsLeaf = true;
        if (
          typeof _specTreeData !== "undefined" &&
          _specTreeData &&
          _specTreeData.nodes
        ) {
          var tn = _specTreeData.nodes.find(function (n) {
            return n.path === _focusedSpecPath;
          });
          if (tn) specIsLeaf = tn.is_leaf;
        }
        dispatchBtn.classList.toggle(
          "hidden",
          !(isValidated && specIsLeaf) || isArchived,
        );
      }

      // Show breakdown button for validated specs that could be decomposed.
      var breakdownBtn = document.getElementById("spec-summarize-btn");
      if (breakdownBtn) {
        var canBreakdown =
          parsed.frontmatter.status === "validated" ||
          parsed.frontmatter.status === "drafted";
        breakdownBtn.textContent = "Break Down";
        breakdownBtn.classList.toggle("hidden", !canBreakdown || isArchived);
        breakdownBtn.onclick = function () {
          breakDownFocusedSpec();
        };
      }

      // Archive button: visible for vague/drafted/complete/stale (status
      // machine allows those four transitions into archived).
      var archiveBtn = document.getElementById("spec-archive-btn");
      if (archiveBtn) {
        var canArchive =
          parsed.frontmatter.status === "vague" ||
          parsed.frontmatter.status === "drafted" ||
          parsed.frontmatter.status === "complete" ||
          parsed.frontmatter.status === "stale";
        archiveBtn.classList.toggle("hidden", !canArchive);
      }

      // Unarchive button: visible only for archived specs.
      var unarchiveBtn = document.getElementById("spec-unarchive-btn");
      if (unarchiveBtn) {
        unarchiveBtn.classList.toggle("hidden", !isArchived);
      }
    })
    .catch(function (err) {
      console.error("spec load error:", err);
      // Stop polling — the spec is unreachable in the current workspace.
      _stopSpecRefreshPoll();
      _focusedSpecPath = null;
      _focusedSpecWorkspace = null;
      _focusedSpecContent = null;
      var ids = [
        "spec-focused-title",
        "spec-focused-status",
        "spec-focused-kind",
        "spec-focused-effort",
        "spec-focused-meta",
      ];
      for (var i = 0; i < ids.length; i++) {
        var el = document.getElementById(ids[i]);
        if (el) {
          el.textContent = "";
          el.className = el.className.replace(/ spec-\S+/g, "");
        }
      }
      var bodyInner = document.getElementById("spec-focused-body-inner");
      if (bodyInner) bodyInner.innerHTML = "";
      teardownFloatingToc();
      var dispatchBtn = document.getElementById("spec-dispatch-btn");
      if (dispatchBtn) dispatchBtn.classList.add("hidden");
      var breakdownBtn = document.getElementById("spec-summarize-btn");
      if (breakdownBtn) breakdownBtn.classList.add("hidden");
      var archiveBtn = document.getElementById("spec-archive-btn");
      if (archiveBtn) archiveBtn.classList.add("hidden");
      var unarchiveBtn = document.getElementById("spec-unarchive-btn");
      if (unarchiveBtn) unarchiveBtn.classList.add("hidden");
      var archivedBanner = document.getElementById("spec-archived-banner");
      if (archivedBanner) archivedBanner.classList.add("hidden");
      if (
        location.hash &&
        (location.hash.indexOf("#plan/") === 0 ||
          location.hash.indexOf("#spec/") === 0)
      ) {
        history.replaceState(null, "", location.pathname);
      }
    });
}

function _startSpecRefreshPoll() {
  _stopSpecRefreshPoll();
  _specRefreshTimer = setInterval(function () {
    _loadAndRenderSpec();
  }, 2000);
}

function _stopSpecRefreshPoll() {
  if (_specRefreshTimer) {
    clearInterval(_specRefreshTimer);
    _specRefreshTimer = null;
  }
}

// parseSpecFrontmatter extracts YAML frontmatter and markdown body from spec text.
function parseSpecFrontmatter(text) {
  if (!text) return { frontmatter: {}, body: "" };
  var match = text.match(/^---\n([\s\S]*?)\n---\n([\s\S]*)$/);
  if (!match) return { frontmatter: {}, body: text };

  var fm = {};
  var lines = match[1].split("\n");
  for (var i = 0; i < lines.length; i++) {
    var line = lines[i];
    var colonIdx = line.indexOf(":");
    if (colonIdx === -1) continue;
    var key = line.substring(0, colonIdx).trim();
    var val = line.substring(colonIdx + 1).trim();
    if (key && val && !val.startsWith("-") && val !== "|" && val !== ">") {
      fm[key] = val;
    }
  }

  return { frontmatter: fm, body: match[2] };
}

// --- Spec pane visibility based on spec availability ---

// _updateSpecPaneVisibility is called after the spec tree loads. It syncs
// the shared specModeState and delegates to the layout state machine.
// Kept as a named export because existing callers (spec-explorer.js) and
// tests reference it by name.
function _updateSpecPaneVisibility(hasSpecs) {
  // Treat "hasSpecs === true" as "there is at least one spec in the tree";
  // the real node array is populated by callers that have access to it.
  if (hasSpecs) {
    if (!specModeState.tree || specModeState.tree.length === 0) {
      specModeState.tree = [{ __synthetic: true }];
    }
  } else {
    specModeState.tree = [];
  }
  _applyLayout();

  // Auto-show README.md as the default focused content when specs exist
  // but nothing is focused yet.
  if (hasSpecs && !_focusedSpecPath) {
    _showSpecReadme();
  }
}

// _showSpecReadme loads and renders specs/README.md in the focused view as
// the default landing content when no specific spec is selected.
function _showSpecReadme() {
  var ws =
    typeof activeWorkspaces !== "undefined" && activeWorkspaces.length > 0
      ? activeWorkspaces[0]
      : null;
  if (!ws) return;

  var absPath = ws + "/specs/README.md";
  var url =
    Routes.explorer.readFile() +
    "?path=" +
    encodeURIComponent(absPath) +
    "&workspace=" +
    encodeURIComponent(ws);

  // Clear header metadata — README is not a spec.
  var titleEl = document.getElementById("spec-focused-title");
  var statusEl = document.getElementById("spec-focused-status");
  var kindEl = document.getElementById("spec-focused-kind");
  var effortEl = document.getElementById("spec-focused-effort");
  var metaEl = document.getElementById("spec-focused-meta");
  // The auto-loaded README acts as the Roadmap landing — match the
  // pinned-explorer entry's "Roadmap" label rather than the legacy
  // "Specs" string left over from the rename.
  if (titleEl) titleEl.textContent = "Roadmap";
  if (statusEl) {
    statusEl.textContent = "";
    statusEl.className = "spec-focused-view__status";
  }
  if (kindEl) {
    kindEl.textContent = "";
    kindEl.className = "spec-focused-view__kind";
  }
  if (effortEl) effortEl.textContent = "";
  if (metaEl) metaEl.textContent = "";
  var dispatchBtn = document.getElementById("spec-dispatch-btn");
  if (dispatchBtn) dispatchBtn.classList.add("hidden");
  var breakdownBtn = document.getElementById("spec-summarize-btn");
  if (breakdownBtn) breakdownBtn.classList.add("hidden");
  // README is not a spec — it has no lifecycle state, so the archive affordances
  // must not leak in from a previously-focused spec.
  var archiveBtn = document.getElementById("spec-archive-btn");
  if (archiveBtn) archiveBtn.classList.add("hidden");
  var unarchiveBtn = document.getElementById("spec-unarchive-btn");
  if (unarchiveBtn) unarchiveBtn.classList.add("hidden");
  var archivedBanner = document.getElementById("spec-archived-banner");
  if (archivedBanner) archivedBanner.classList.add("hidden");

  var innerEl = document.getElementById("spec-focused-body-inner");
  if (innerEl)
    innerEl.innerHTML = '<div class="spec-loading">Loading\u2026</div>';

  fetch(url, { headers: withBearerHeaders() })
    .then(function (res) {
      if (!res.ok) throw new Error("HTTP " + res.status);
      return res.text();
    })
    .then(function (text) {
      if (!innerEl) return;
      innerEl.innerHTML = renderMarkdown(text);
      // Post-process: mermaid diagrams and spec link navigation.
      if (typeof _mdRender !== "undefined" && _mdRender.enhanceMarkdown) {
        _mdRender
          .enhanceMarkdown(innerEl, {
            links: true,
            linkHandler: "spec",
            basePath: "",
            workspace: ws,
          })
          .then(function () {
            var scrollEl = document.getElementById("spec-focused-body");
            var anchorEl = document.getElementById("spec-focused-view");
            if (scrollEl && anchorEl) {
              buildFloatingToc(innerEl, scrollEl, anchorEl, {
                headingSelector: "h1, h2, h3, h4",
                idPrefix: "spec-heading",
              });
            }
          });
      }
    })
    .catch(function () {
      // README.md doesn't exist — show a placeholder.
      if (innerEl) innerEl.innerHTML = "";
      if (titleEl) titleEl.textContent = "Select a spec";
    });
}

// --- Spec mode keyboard shortcut stubs ---

// openSelectedSpec opens the currently selected explorer node in the focused view.
// No-op until spec explorer is wired.
function openSelectedSpec() {}

// dispatchFocusedSpec dispatches the focused spec as a board task via the
// dispatch API. Shows a confirmation prompt, loading state, and refreshes
// the spec view on success.
function dispatchFocusedSpec() {
  if (!_focusedSpecPath) return;

  showConfirm("Dispatch this spec to the task board?").then(
    function (confirmed) {
      if (!confirmed) return;

      var btn = document.getElementById("spec-dispatch-btn");
      if (btn) {
        btn.disabled = true;
        btn.textContent = "Dispatching\u2026";
      }

      api(Routes.specs.dispatch(), {
        method: "POST",
        body: JSON.stringify({ paths: [_focusedSpecPath], run: false }),
      })
        .then(function (resp) {
          // Hide the dispatch button (spec is no longer validated after dispatch).
          if (btn) btn.classList.add("hidden");
          // Refresh the focused spec view to reflect the new dispatched_task_id.
          _loadAndRenderSpec();
          // Surface a dispatch-complete toast with a "View on Board →"
          // affordance, per the plan-to-board-bridges spec.
          var taskIds = [];
          if (resp && Array.isArray(resp.dispatched)) {
            for (var i = 0; i < resp.dispatched.length; i++) {
              if (resp.dispatched[i] && resp.dispatched[i].task_id) {
                taskIds.push(resp.dispatched[i].task_id);
              }
            }
          }
          if (typeof showDispatchCompleteToast === "function") {
            showDispatchCompleteToast(taskIds);
          }
        })
        .catch(function (err) {
          showAlert("Dispatch failed: " + err.message);
        })
        .finally(function () {
          if (btn) {
            btn.disabled = false;
            btn.textContent = "Dispatch";
          }
        });
    },
  );
}

// breakDownFocusedSpec sends the /break-down slash command via the planning chat.
function breakDownFocusedSpec() {
  if (typeof PlanningChat !== "undefined") {
    PlanningChat.sendMessage("/break-down");
  }
}

// --- Archive / unarchive focused spec ---

// _lastArchiveAction stores the most recent archive/unarchive so the Undo
// button in the toast can reverse it. Shape: {action, path, prevStatus}.
// New toasts overwrite this; dismissing the latest toast clears it.
var _lastArchiveAction = null;
// _archiveToastCount is a monotonically increasing id used to key timers.
var _archiveToastCount = 0;

function _focusedSpecHasChildren() {
  if (!_focusedSpecPath) return false;
  if (
    typeof _specTreeData === "undefined" ||
    !_specTreeData ||
    !_specTreeData.nodes
  ) {
    return false;
  }
  var node = _specTreeData.nodes.find(function (n) {
    return n.path === _focusedSpecPath;
  });
  return !!(node && !node.is_leaf && node.children && node.children.length > 0);
}

function _focusedSpecChildCount() {
  if (!_focusedSpecPath) return 0;
  if (
    typeof _specTreeData === "undefined" ||
    !_specTreeData ||
    !_specTreeData.nodes
  ) {
    return 0;
  }
  var node = _specTreeData.nodes.find(function (n) {
    return n.path === _focusedSpecPath;
  });
  if (!node || node.is_leaf) return 0;
  // Count all descendants (transitive) in the tree.
  var count = 0;
  var queue = (node.children || []).slice();
  while (queue.length > 0) {
    var path = queue.shift();
    count++;
    var child = _specTreeData.nodes.find(function (n) {
      return n.path === path;
    });
    if (child && child.children) {
      for (var i = 0; i < child.children.length; i++) {
        queue.push(child.children[i]);
      }
    }
  }
  return count;
}

function archiveFocusedSpec() {
  if (!_focusedSpecPath) return;
  var prevStatus = null;
  if (_focusedSpecContent) {
    var parsed = parseSpecFrontmatter(_focusedSpecContent);
    prevStatus = parsed.frontmatter.status || null;
  }
  var proceed = Promise.resolve(true);
  if (_focusedSpecHasChildren()) {
    var n = _focusedSpecChildCount();
    proceed = showConfirm(
      "Archiving will hide " + n + " descendant spec(s). Continue?",
    );
  }
  var path = _focusedSpecPath;
  proceed.then(function (ok) {
    if (!ok) return;
    _callArchiveEndpoint("/api/specs/archive", path).then(function (res) {
      if (!res.ok) return;
      var action = { action: "archive", path: path, prevStatus: prevStatus };
      _lastArchiveAction = action;
      _showArchiveToast("Spec archived: " + path, action);
      _loadAndRenderSpec();
    });
  });
}

function unarchiveFocusedSpec() {
  if (!_focusedSpecPath) return;
  var path = _focusedSpecPath;
  _callArchiveEndpoint("/api/specs/unarchive", path).then(function (res) {
    if (!res.ok) return;
    var action = {
      action: "unarchive",
      path: path,
      prevStatus: "archived",
    };
    _lastArchiveAction = action;
    _showArchiveToast("Spec unarchived: " + path, action);
    _loadAndRenderSpec();
  });
}

// undoArchiveAction reverses the most recent archive/unarchive. Kept as a
// global for back-compat (tests call it without args); per-toast undo buttons
// call _undoAction directly so they reverse the specific toast's action even
// when a later toast has moved _lastArchiveAction on.
function undoArchiveAction() {
  var last = _lastArchiveAction;
  if (!last) return;
  _undoAction(last, null);
}

function _undoAction(action, toastEl) {
  var reverseUrl =
    action.action === "archive" ? "/api/specs/unarchive" : "/api/specs/archive";
  _callArchiveEndpoint(reverseUrl, action.path).then(function (res) {
    if (!res.ok) return;
    if (_lastArchiveAction === action) _lastArchiveAction = null;
    if (toastEl) _dismissToast(toastEl);
    _loadAndRenderSpec();
  });
}

// dismissArchiveToast clears the most recent toast and its pending action.
// Kept as a named global so existing tests and callers still resolve.
function dismissArchiveToast() {
  _lastArchiveAction = null;
  var container = document.getElementById("spec-archive-toasts");
  if (!container) return;
  // Clear all toasts — single-arg dismiss is the "close everything" path.
  while (container.firstChild) {
    _dismissToast(container.firstChild);
  }
}

function _dismissToast(toastEl) {
  if (!toastEl) return;
  if (toastEl._dismissTimer) {
    clearTimeout(toastEl._dismissTimer);
    toastEl._dismissTimer = null;
  }
  if (toastEl.parentNode) toastEl.parentNode.removeChild(toastEl);
}

function _showArchiveToast(message, action) {
  var container = document.getElementById("spec-archive-toasts");
  if (!container || !container.appendChild) return;

  _archiveToastCount++;
  var toast = document.createElement("div");
  toast.className = "spec-archive-toast";

  var textEl = document.createElement("span");
  textEl.className = "spec-archive-toast__text";
  textEl.textContent = message;
  toast.appendChild(textEl);

  if (action) {
    var undoBtn = document.createElement("button");
    undoBtn.className = "spec-summarize-btn";
    undoBtn.textContent = "Undo";
    undoBtn.addEventListener("click", function () {
      _undoAction(action, toast);
    });
    toast.appendChild(undoBtn);
  }

  var closeBtn = document.createElement("button");
  closeBtn.className = "spec-archive-toast__close";
  closeBtn.setAttribute("aria-label", "Dismiss");
  closeBtn.textContent = "\u2715";
  closeBtn.addEventListener("click", function () {
    _dismissToast(toast);
  });
  toast.appendChild(closeBtn);

  container.appendChild(toast);
  toast._dismissTimer = setTimeout(function () {
    _dismissToast(toast);
  }, 8000);
}

function _callArchiveEndpoint(url, path) {
  var opts = {
    method: "POST",
    headers: withBearerHeaders({ "Content-Type": "application/json" }),
    body: JSON.stringify({ path: path }),
  };
  return fetch(url, opts).then(function (res) {
    if (!res.ok) {
      return res.text().then(function (t) {
        showAlert((t || "Request failed").trim());
        return { ok: false };
      });
    }
    return { ok: true };
  });
}

// --- Chat pane toggle ---

var _specChatOpenKey = "wallfacer-spec-chat-open";

// _syncSpecChatToggle keeps the header Chat button's pressed state and
// tooltip in sync with the actual pane visibility so screen readers hear
// "pressed / not pressed" and sighted users see a consistent affordance
// whether the pane is open or folded.
function _syncSpecChatToggle(isOpen) {
  var btn = document.getElementById("spec-chat-toggle-btn");
  if (!btn) return;
  btn.setAttribute("aria-pressed", isOpen ? "true" : "false");
  btn.title = isOpen ? "Hide chat pane (C)" : "Show chat pane (C)";
  btn.classList.toggle("spec-chat-toggle-btn--folded", !isOpen);
}

// toggleSpecChat shows/hides the chat pane and its resize handle.
// When opened, auto-focuses the chat input.
function toggleSpecChat() {
  var chatStream = document.getElementById("spec-chat-stream");
  var resizeHandle = document.getElementById("spec-chat-resize");
  if (!chatStream) return;

  var isHidden = chatStream.style.display === "none";
  chatStream.style.display = isHidden ? "" : "none";
  if (resizeHandle) resizeHandle.style.display = isHidden ? "" : "none";
  localStorage.setItem(_specChatOpenKey, isHidden ? "1" : "0");
  _syncSpecChatToggle(isHidden);

  // Auto-focus the input when opening.
  if (isHidden) {
    var input = document.getElementById("spec-chat-input");
    if (input) input.focus();
  }
}

function _restoreSpecChatState() {
  var saved = localStorage.getItem(_specChatOpenKey);
  // Default to open if no saved state.
  if (saved === "0") {
    var chatStream = document.getElementById("spec-chat-stream");
    var resizeHandle = document.getElementById("spec-chat-resize");
    if (chatStream) chatStream.style.display = "none";
    if (resizeHandle) resizeHandle.style.display = "none";
    _syncSpecChatToggle(false);
  } else {
    _syncSpecChatToggle(true);
  }
}

// --- Chat pane resize ---

var _specChatMinWidth = 280;
var _specChatMaxWidthFraction = 0.5;
var _specChatStorageKey = "wallfacer-spec-chat-width";

function _initSpecChatResize() {
  var handle = document.getElementById("spec-chat-resize");
  var chatPane = document.getElementById("spec-chat-stream");
  if (!handle || !chatPane) return;

  // Restore persisted width.
  var stored = localStorage.getItem(_specChatStorageKey);
  if (stored) {
    var w = parseInt(stored, 10);
    if (w >= _specChatMinWidth) chatPane.style.width = w + "px";
  }

  handle.addEventListener("mousedown", function (e) {
    e.preventDefault();
    var startX = e.clientX;
    var startW = chatPane.offsetWidth;
    document.body.style.userSelect = "none";
    document.body.style.cursor = "col-resize";

    function onMouseMove(ev) {
      // Chat is on the right, so dragging left increases width.
      var delta = startX - ev.clientX;
      var maxW = Math.floor(window.innerWidth * _specChatMaxWidthFraction);
      var newW = Math.min(maxW, Math.max(_specChatMinWidth, startW + delta));
      chatPane.style.width = newW + "px";
    }

    function onMouseUp() {
      document.removeEventListener("mousemove", onMouseMove);
      document.removeEventListener("mouseup", onMouseUp);
      document.body.style.userSelect = "";
      document.body.style.cursor = "";
      localStorage.setItem(
        _specChatStorageKey,
        parseInt(chatPane.style.width, 10),
      );
      // Trigger TOC relayout after chat pane resize.
      window.dispatchEvent(new Event("resize"));
    }

    document.addEventListener("mousemove", onMouseMove);
    document.addEventListener("mouseup", onMouseUp);
  });
}

// Restore persisted mode on page load. Uses _applyMode directly (not
// switchMode) because the JS variable already matches localStorage but
// the DOM hasn't been updated yet — switchMode's idempotency guard
// would skip the update.
document.addEventListener("DOMContentLoaded", function () {
  _restoreSidebarState();
  if (currentMode !== "board") {
    _applyMode(currentMode);
  }
  _restoreSpecChatState();
  _initSpecChatResize();
  if (typeof PlanningChat !== "undefined") {
    PlanningChat.init();
  }
});

// Expose openPlanForTask globally so explorer.js can call it.
if (typeof window !== "undefined") {
  window.openPlanForTask = openPlanForTask;
}
