// --- Spec mode state and switching ---

// currentMode: "board" | "spec" | "docs"
var _validModes = { board: true, spec: true, docs: true };
var currentMode = _validModes[localStorage.getItem("wallfacer-mode")]
  ? localStorage.getItem("wallfacer-mode")
  : "board";

function getCurrentMode() {
  return currentMode;
}

function setCurrentMode(mode) {
  currentMode = mode;
  localStorage.setItem("wallfacer-mode", mode);
}

// _applyMode updates the DOM to reflect the given mode without checking
// whether the mode has changed. Used both by switchMode (after the
// idempotency check) and by DOMContentLoaded (to restore persisted mode
// where the JS variable already matches but the DOM hasn't been touched).
function _applyMode(mode) {
  // Update sidebar navigation active states.
  var boardNav = document.getElementById("sidebar-nav-board");
  var specNav = document.getElementById("sidebar-nav-spec");
  var docsNav = document.getElementById("sidebar-nav-docs");
  if (boardNav) boardNav.classList.toggle("active", mode === "board");
  if (specNav) specNav.classList.toggle("active", mode === "spec");
  if (docsNav) docsNav.classList.toggle("active", mode === "docs");

  // Toggle main content areas.
  var board = document.getElementById("board");
  var specView = document.getElementById("spec-mode-container");
  var docsView = document.getElementById("docs-mode-container");
  if (board) board.style.display = mode === "board" ? "" : "none";
  if (specView) specView.style.display = mode === "spec" ? "" : "none";
  if (docsView) docsView.style.display = mode === "docs" ? "" : "none";

  // Stop spec refresh poll and clear hash when leaving spec mode.
  if (mode !== "spec") {
    _stopSpecRefreshPoll();
    if (
      typeof location !== "undefined" &&
      location.hash &&
      location.hash.indexOf("#spec/") === 0
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
  if (header) header.style.display = mode === "docs" ? "none" : "";
  if (gitBar) gitBar.style.display = mode === "docs" ? "none" : "";

  // Update search placeholder based on mode.
  var searchInput = document.getElementById("task-search");
  if (searchInput) {
    searchInput.placeholder =
      mode === "spec"
        ? "Filter specs\u2026"
        : "Filter tasks\u2026 or @search server";
  }

  // Clear spec text filter when leaving spec mode.
  if (mode !== "spec" && typeof setSpecTextFilter === "function") {
    setSpecTextFilter("");
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
// nav active states, swaps main content visibility, and persists the choice.
function switchMode(mode) {
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

// --- Focused spec view ---

var _focusedSpecPath = null;
var _focusedSpecWorkspace = null;
var _focusedSpecContent = null;
var _specRefreshTimer = null;

// focusSpec loads and renders a spec file in the focused markdown view.
function focusSpec(specPath, workspace) {
  _focusedSpecPath = specPath;
  _focusedSpecWorkspace = workspace;
  _focusedSpecContent = null; // reset so loading indicator shows

  // Show loading state in the focused view.
  var titleEl = document.getElementById("spec-focused-title");
  var innerEl = document.getElementById("spec-focused-body-inner");
  if (titleEl) titleEl.textContent = specPath;
  if (innerEl)
    innerEl.innerHTML = '<div class="spec-loading">Loading\u2026</div>';

  _loadAndRenderSpec();
  _startSpecRefreshPoll();
  // Update hash for deep-linking.
  history.replaceState(null, "", "#spec/" + encodeURIComponent(specPath));
  // Update dependency minimap.
  if (
    typeof renderMinimap === "function" &&
    typeof _specTreeData !== "undefined"
  ) {
    renderMinimap(specPath, _specTreeData);
  }
}

function getFocusedSpecPath() {
  return _focusedSpecPath;
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

  // The spec tree returns paths relative to specs/ (e.g., "local/foo.md").
  // The explorer file API expects an absolute path within the workspace.
  var absPath = ws + "/specs/" + _focusedSpecPath;
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
        dispatchBtn.classList.toggle("hidden", !(isValidated && specIsLeaf));
      }

      // Show breakdown button for validated specs that could be decomposed.
      var breakdownBtn = document.getElementById("spec-summarize-btn");
      if (breakdownBtn) {
        var canBreakdown =
          parsed.frontmatter.status === "validated" ||
          parsed.frontmatter.status === "drafted";
        breakdownBtn.textContent = "Break Down";
        breakdownBtn.classList.toggle("hidden", !canBreakdown);
        breakdownBtn.onclick = function () {
          breakDownFocusedSpec();
        };
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
      if (location.hash && location.hash.indexOf("#spec/") === 0) {
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

// _updateSpecPaneVisibility is called after the spec tree loads. If specs
// exist, the three-pane layout (explorer + focused view + chat) is shown.
// If no specs exist, only the chat pane is shown (full width).
function _updateSpecPaneVisibility(hasSpecs) {
  var container = document.getElementById("spec-mode-container");
  var explorerPanel = document.getElementById("explorer-panel");

  if (container) {
    container.classList.toggle("spec-mode--chat-only", !hasSpecs);
  }
  if (explorerPanel) {
    explorerPanel.style.display =
      hasSpecs && getCurrentMode() === "spec" ? "" : "none";
  }

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
  if (titleEl) titleEl.textContent = "Specs";
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

// dispatchFocusedSpec dispatches the focused spec as a kanban task via the
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
        .then(function () {
          // Hide the dispatch button (spec is no longer validated after dispatch).
          if (btn) btn.classList.add("hidden");
          // Refresh the focused spec view to reflect the new dispatched_task_id.
          _loadAndRenderSpec();
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

// --- Chat pane toggle ---

var _specChatOpenKey = "wallfacer-spec-chat-open";

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
