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

// switchMode toggles between board, spec, and docs modes. Updates sidebar
// nav active states, swaps main content visibility, and persists the choice.
function switchMode(mode) {
  if (mode === currentMode) return;
  setCurrentMode(mode);
  _applyMode(mode);
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
            _buildSpecToc(bodyEl);
          });
      }

      // Show dispatch button for any validated spec (design or implementation).
      if (dispatchBtn) {
        var isValidated = parsed.frontmatter.status === "validated";
        dispatchBtn.classList.toggle("hidden", !isValidated);
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
      var tocEl = document.getElementById("spec-toc");
      if (tocEl) tocEl.remove();
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

// _buildSpecToc extracts headings from the rendered markdown body and
// builds a floating table of contents in the top-right of the focused view.
// A scroll-driven exclusion zone dynamically adds margin-right to body
// elements that overlap the TOC so text reflows around it on scroll.
function _buildSpecToc(bodyEl) {
  // Remove existing TOC and detach previous scroll listener.
  var existing = document.getElementById("spec-toc");
  if (existing) existing.remove();
  _teardownTocExclusion();

  var headings = bodyEl.querySelectorAll("h1, h2, h3, h4");
  if (!headings || headings.length < 2) return;

  var toc = document.createElement("div");
  toc.id = "spec-toc";
  toc.className = "spec-toc";

  var tocTitle = document.createElement("div");
  tocTitle.className = "spec-toc__title";
  tocTitle.textContent = "Contents";
  toc.appendChild(tocTitle);

  for (var i = 0; i < headings.length; i++) {
    var h = headings[i];
    // Give headings an id if they don't have one.
    if (!h.id) {
      h.id =
        "spec-heading-" +
        h.textContent
          .toLowerCase()
          .replace(/[^a-z0-9]+/g, "-")
          .replace(/^-|-$/g, "");
    }
    var level = parseInt(h.tagName.substring(1), 10);
    var link = document.createElement("a");
    link.className = "spec-toc__link spec-toc__link--h" + level;
    link.href = "#" + h.id;
    link.textContent = h.textContent;
    link.addEventListener(
      "click",
      (function (targetId) {
        return function (e) {
          e.preventDefault();
          var target = document.getElementById(targetId);
          if (target) target.scrollIntoView({ behavior: "smooth" });
        };
      })(h.id),
    );
    toc.appendChild(link);
  }

  // Append to spec-focused-view (not bodyEl) so it stays fixed on scroll.
  var focusedView = document.getElementById("spec-focused-view");
  if (focusedView) {
    focusedView.appendChild(toc);
  } else {
    bodyEl.appendChild(toc);
  }

  // Start exclusion-zone tracking.
  _setupTocExclusion();
}

// --- TOC exclusion zone (Pretext-powered) ---
//
// Uses @chenglou/pretext to predict paragraph heights at any width via
// pure arithmetic — no DOM reflow.  The workflow follows pretext's design:
//
//   prepare()  — one-time per spec load (expensive: measures text segments)
//   layout()   — cheap hot path rerun on every scroll frame and resize
//
// For non-text blocks (tables, pre, lists, etc.) we fall back to DOM
// measurement, but only on resize — not per scroll frame.

var _tocItems = null; // array of per-block data, survives resize
var _tocScrollHandler = null;
var _tocScrollRaf = null;
var _tocResizeHandler = null;
var _tocResizeTimer = null;
var _tocLayout = null; // width-dependent data, rebuilt on resize

function _setupTocExclusion() {
  var scrollEl = document.getElementById("spec-focused-body");
  var innerEl = document.getElementById("spec-focused-body-inner");
  var toc = document.getElementById("spec-toc");
  if (!scrollEl || !innerEl || !toc) return;

  _tocPrepare(scrollEl, innerEl);
  _tocRelayout(scrollEl, innerEl, toc);
  _tocApply();

  _tocScrollHandler = function () {
    if (_tocScrollRaf) return;
    _tocScrollRaf = requestAnimationFrame(function () {
      _tocScrollRaf = null;
      _tocApply();
    });
  };
  scrollEl.addEventListener("scroll", _tocScrollHandler);

  _tocResizeHandler = function () {
    clearTimeout(_tocResizeTimer);
    _tocResizeTimer = setTimeout(function () {
      var s = document.getElementById("spec-focused-body");
      var inn = document.getElementById("spec-focused-body-inner");
      var t = document.getElementById("spec-toc");
      if (!s || !inn || !t) return;
      _tocRelayout(s, inn, t);
      _tocApply();
    }, 100);
  };
  window.addEventListener("resize", _tocResizeHandler);
}

// _tocPrepare runs once per spec load.  Calls pretext.prepare() for every
// text block and caches the prepared handle + font metrics.  Non-text blocks
// store a DOM reference for later measurement.
//
// scrollEl = outer scroll container (#spec-focused-body)
// innerEl  = centered content wrapper (#spec-focused-body-inner)
function _tocPrepare(scrollEl, innerEl) {
  var pt = window.pretext || null;
  var blocks = innerEl.querySelectorAll(":scope > *");
  if (blocks.length === 0) return;

  var innerCS = getComputedStyle(innerEl);
  var padT = parseFloat(innerCS.paddingTop) || 0;
  var scrollRect = scrollEl.getBoundingClientRect();

  var items = [];
  for (var i = 0; i < blocks.length; i++) {
    var block = blocks[i];
    var rect = block.getBoundingClientRect();
    var contentY = rect.top - scrollRect.top + scrollEl.scrollTop - padT;
    var isText = block.tagName === "P" || /^H[1-6]$/.test(block.tagName);
    var item = { el: block, contentY: contentY };

    if (isText && pt) {
      var cs = getComputedStyle(block);
      var font = cs.fontWeight + " " + cs.fontSize + " " + cs.fontFamily;
      var lh = parseFloat(cs.lineHeight);
      if (isNaN(lh)) lh = parseFloat(cs.fontSize) * 1.7;
      try {
        item.prepared = pt.prepare(block.textContent || "", font);
        item.lineHeight = lh;
        item.overhead = Math.max(
          0,
          rect.height - pt.layout(item.prepared, rect.width, lh).height,
        );
      } catch (_e) {
        item.prepared = null;
      }
    }
    items.push(item);
  }

  // Record initial heights and derive inter-element gaps.
  for (var k = 0; k < items.length; k++) {
    items[k].heightAtSetup = items[k].el.getBoundingClientRect().height;
  }
  for (var j = 0; j < items.length; j++) {
    if (j === 0) {
      items[j].gap = items[j].contentY;
    } else {
      var pe = items[j - 1].contentY + items[j - 1].heightAtSetup;
      items[j].gap = Math.max(0, items[j].contentY - pe);
    }
  }

  _tocItems = items;
}

// _tocRelayout recomputes width-dependent data.  For text blocks it only
// calls pretext.layout() (pure arithmetic).  For non-text blocks it does
// one DOM measurement.
function _tocRelayout(scrollEl, innerEl, toc) {
  if (!_tocItems) return;

  // Clear stale constraints before measuring.
  for (var c = 0; c < _tocItems.length; c++) {
    _tocItems[c].el.style.maxWidth = "";
  }

  var pt = window.pretext || null;
  var tocW = toc.offsetWidth + 24;
  var innerCS = getComputedStyle(innerEl);
  var padL = parseFloat(innerCS.paddingLeft) || 0;
  var padR = parseFloat(innerCS.paddingRight) || 0;
  var padT = parseFloat(innerCS.paddingTop) || 0;
  var fullWidth = innerEl.clientWidth - padL - padR;
  var narrowWidth = Math.max(fullWidth - tocW, 80);

  var tocRect = toc.getBoundingClientRect();
  var innerRect = innerEl.getBoundingClientRect();

  // If the inner column doesn't reach the TOC, no exclusion needed.
  if (tocRect.left >= innerRect.right) {
    _tocLayout = null;
    return;
  }

  var scrollRect = scrollEl.getBoundingClientRect();
  var tocScrollTop = tocRect.top - scrollRect.top;
  var tocScrollBottom = tocRect.bottom - scrollRect.top;

  for (var i = 0; i < _tocItems.length; i++) {
    var item = _tocItems[i];

    if (item.prepared && pt) {
      item.heightFull =
        pt.layout(item.prepared, fullWidth, item.lineHeight).height +
        item.overhead;
      item.heightNarrow =
        pt.layout(item.prepared, narrowWidth, item.lineHeight).height +
        item.overhead;
    } else {
      item.heightFull = item.el.getBoundingClientRect().height;
      var origMW = item.el.style.maxWidth;
      item.el.style.maxWidth = narrowWidth + "px";
      item.heightNarrow = item.el.getBoundingClientRect().height;
      item.el.style.maxWidth = origMW;
    }
  }

  _tocLayout = {
    narrowWidth: narrowWidth,
    tocScrollTop: tocScrollTop,
    tocScrollBottom: tocScrollBottom,
    innerPadTop: padT,
    scrollEl: scrollEl,
  };
}

// _tocApply runs per scroll frame.  Pure arithmetic for pretext blocks;
// only sets/clears max-width on DOM elements that change state.
function _tocApply() {
  if (!_tocLayout || !_tocItems) return;
  var d = _tocLayout;
  var scrollTop = d.scrollEl.scrollTop;
  var tocTop = scrollTop + d.tocScrollTop - d.innerPadTop;
  var tocBottom = scrollTop + d.tocScrollBottom - d.innerPadTop;

  var y = 0;
  for (var i = 0; i < _tocItems.length; i++) {
    var item = _tocItems[i];
    y += item.gap;
    var overlap = y + item.heightFull > tocTop && y < tocBottom;
    if (overlap) {
      item.el.style.maxWidth = d.narrowWidth + "px";
      y += item.heightNarrow;
    } else {
      item.el.style.maxWidth = "";
      y += item.heightFull;
    }
  }
}

function _teardownTocExclusion() {
  if (_tocLayout) {
    var scrollEl = _tocLayout.scrollEl;
    if (scrollEl && _tocScrollHandler) {
      scrollEl.removeEventListener("scroll", _tocScrollHandler);
    }
  }
  if (_tocItems) {
    for (var i = 0; i < _tocItems.length; i++) {
      _tocItems[i].el.style.maxWidth = "";
    }
  }
  _tocScrollHandler = null;
  _tocItems = null;
  _tocLayout = null;
  if (_tocScrollRaf) {
    cancelAnimationFrame(_tocScrollRaf);
    _tocScrollRaf = null;
  }
  if (_tocResizeHandler) {
    window.removeEventListener("resize", _tocResizeHandler);
    _tocResizeHandler = null;
  }
  clearTimeout(_tocResizeTimer);
  _tocResizeTimer = null;
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
            _buildSpecToc(innerEl);
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

// dispatchFocusedSpec dispatches the focused leaf spec as a kanban task.
// No-op stub — wired by dispatch-workflow spec.
function dispatchFocusedSpec() {}

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
      var s = document.getElementById("spec-focused-body");
      var inn = document.getElementById("spec-focused-body-inner");
      var t = document.getElementById("spec-toc");
      if (s && inn && t) {
        _tocRelayout(s, inn, t);
        _tocApply();
      }
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
