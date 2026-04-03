// --- Spec mode state and switching ---

// currentMode: "board" | "spec"
var currentMode =
  localStorage.getItem("wallfacer-mode") === "spec" ? "spec" : "board";

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
  // Update header mode tabs.
  var boardTab = document.getElementById("mode-tab-board");
  var specTab = document.getElementById("mode-tab-spec");
  if (boardTab) boardTab.classList.toggle("active", mode === "board");
  if (specTab) specTab.classList.toggle("active", mode === "spec");

  // Toggle main content areas.
  var board = document.getElementById("board");
  var specView = document.getElementById("spec-mode-container");
  if (board) board.style.display = mode === "board" ? "" : "none";
  if (specView) specView.style.display = mode === "spec" ? "" : "none";

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

  // Switch explorer root (no-op until spec-explorer is wired).
  if (typeof switchExplorerRoot === "function") {
    switchExplorerRoot(mode === "spec" ? "specs" : "workspace");
  }

  // Auto-show explorer in spec mode, auto-hide in board mode.
  var explorerPanel = document.getElementById("explorer-panel");
  if (explorerPanel) {
    if (mode === "spec") {
      explorerPanel.style.display = "";
    } else {
      explorerPanel.style.display = "none";
    }
  }
}

// switchMode toggles between board and spec mode. Updates header tabs,
// swaps main content visibility, and persists the choice.
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
  var bodyEl = document.getElementById("spec-focused-body");
  if (titleEl) titleEl.textContent = specPath;
  if (bodyEl)
    bodyEl.innerHTML = '<div class="spec-loading">Loading\u2026</div>';

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
      var bodyEl = document.getElementById("spec-focused-body");
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
      var bodyEl = document.getElementById("spec-focused-body");
      if (bodyEl) bodyEl.innerHTML = "";
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
// Uses @chenglou/pretext (vendored at js/vendor/pretext.min.js) to predict
// paragraph heights at narrowed widths without touching the DOM, then
// simulates block layout top-down per scroll frame to determine which
// elements overlap the TOC.
// For tables, max-width is used instead of margin-right.

var _tocExclusionRaf = null;
var _tocExclusionHandler = null;
var _tocExclusion = null; // prepared data for the current spec

function _setupTocExclusion() {
  var bodyEl = document.getElementById("spec-focused-body");
  var toc = document.getElementById("spec-toc");
  if (!bodyEl || !toc) return;

  var pt = window.pretext || null;
  _buildExclusionData(pt, bodyEl, toc);
  _applyTocExclusion();
  _tocExclusionHandler = function () {
    if (_tocExclusionRaf) return;
    _tocExclusionRaf = requestAnimationFrame(function () {
      _tocExclusionRaf = null;
      _applyTocExclusion();
    });
  };
  bodyEl.addEventListener("scroll", _tocExclusionHandler);
}

function _buildExclusionData(pt, bodyEl, toc) {
  var blocks = bodyEl.querySelectorAll(":scope > *");
  if (blocks.length === 0) return;

  var tocW = toc.offsetWidth + 24;
  var bodyCS = getComputedStyle(bodyEl);
  var padL = parseFloat(bodyCS.paddingLeft) || 0;
  var padR = parseFloat(bodyCS.paddingRight) || 0;
  var padT = parseFloat(bodyCS.paddingTop) || 0;
  var fullWidth = bodyEl.clientWidth - padL - padR;
  var narrowWidth = Math.max(fullWidth - tocW, 80);

  // TOC position relative to bodyEl's top edge (constant across scrolls).
  var tocRect = toc.getBoundingClientRect();
  var bodyRect = bodyEl.getBoundingClientRect();
  var tocBodyTop = tocRect.top - bodyRect.top;
  var tocBodyBottom = tocRect.bottom - bodyRect.top;

  // Snapshot initial element positions and compute predicted heights.
  var items = [];
  for (var i = 0; i < blocks.length; i++) {
    var block = blocks[i];
    var rect = block.getBoundingClientRect();
    var contentY = rect.top - bodyRect.top + bodyEl.scrollTop - padT;
    var heightFull = rect.height;
    var isTable = block.tagName === "TABLE";
    var isText = block.tagName === "P" || /^H[1-6]$/.test(block.tagName);
    var heightNarrow;

    if (isText && pt) {
      // Use Pretext: predict height at both widths, derive the non-text
      // overhead (padding, border) from the difference.
      var cs = getComputedStyle(block);
      var font = cs.fontWeight + " " + cs.fontSize + " " + cs.fontFamily;
      var lh = parseFloat(cs.lineHeight);
      if (isNaN(lh)) lh = parseFloat(cs.fontSize) * 1.7;
      try {
        var prepared = pt.prepare(block.textContent || "", font);
        var textHeightFull = pt.layout(prepared, fullWidth, lh).height;
        var textHeightNarrow = pt.layout(prepared, narrowWidth, lh).height;
        var overhead = heightFull - textHeightFull;
        if (overhead < 0) overhead = 0;
        heightNarrow = textHeightNarrow + overhead;
      } catch (_e) {
        heightNarrow = heightFull;
      }
    } else {
      // DOM measurement: temporarily apply the constraint, read height, revert.
      if (isTable) {
        var origMW = block.style.maxWidth;
        block.style.maxWidth = narrowWidth + "px";
        heightNarrow = block.getBoundingClientRect().height;
        block.style.maxWidth = origMW;
      } else {
        var origMR = block.style.marginRight;
        block.style.marginRight = tocW + "px";
        heightNarrow = block.getBoundingClientRect().height;
        block.style.marginRight = origMR;
      }
    }

    items.push({
      el: block,
      contentY: contentY,
      heightFull: heightFull,
      heightNarrow: heightNarrow,
      isTable: isTable,
    });
  }

  // Derive gaps between consecutive elements from the initial layout.
  for (var j = 0; j < items.length; j++) {
    if (j === 0) {
      items[j].gap = items[j].contentY;
    } else {
      var prevEnd = items[j - 1].contentY + items[j - 1].heightFull;
      items[j].gap = Math.max(0, items[j].contentY - prevEnd);
    }
  }

  _tocExclusion = {
    items: items,
    tocWidth: tocW,
    narrowWidth: narrowWidth,
    tocBodyTop: tocBodyTop,
    tocBodyBottom: tocBodyBottom,
    bodyPadTop: padT,
    bodyEl: bodyEl,
  };
}

function _applyTocExclusion() {
  if (!_tocExclusion) return;
  var d = _tocExclusion;
  var scrollTop = d.bodyEl.scrollTop;

  // TOC zone in content-space coordinates.
  var tocTop = scrollTop + d.tocBodyTop - d.bodyPadTop;
  var tocBottom = scrollTop + d.tocBodyBottom - d.bodyPadTop;

  // Simulate layout top-down using pre-computed heights.
  var y = 0;
  for (var i = 0; i < d.items.length; i++) {
    var item = d.items[i];
    y += item.gap;
    var overlap = y + item.heightFull > tocTop && y < tocBottom;
    if (overlap) {
      if (item.isTable) {
        item.el.style.maxWidth = d.narrowWidth + "px";
        item.el.style.marginRight = "";
      } else {
        item.el.style.marginRight = d.tocWidth + "px";
        item.el.style.maxWidth = "";
      }
      y += item.heightNarrow;
    } else {
      item.el.style.marginRight = "";
      item.el.style.maxWidth = "";
      y += item.heightFull;
    }
  }
}

function _teardownTocExclusion() {
  if (_tocExclusion) {
    var bodyEl = _tocExclusion.bodyEl;
    if (bodyEl && _tocExclusionHandler) {
      bodyEl.removeEventListener("scroll", _tocExclusionHandler);
    }
    for (var i = 0; i < _tocExclusion.items.length; i++) {
      var el = _tocExclusion.items[i].el;
      el.style.marginRight = "";
      el.style.maxWidth = "";
    }
  }
  _tocExclusionHandler = null;
  _tocExclusion = null;
  if (_tocExclusionRaf) {
    cancelAnimationFrame(_tocExclusionRaf);
    _tocExclusionRaf = null;
  }
}

// --- Spec mode keyboard shortcut stubs ---

// openSelectedSpec opens the currently selected explorer node in the focused view.
// No-op until spec explorer is wired.
function openSelectedSpec() {}

// dispatchFocusedSpec dispatches the focused leaf spec as a kanban task.
// No-op stub — wired by dispatch-workflow spec.
function dispatchFocusedSpec() {}

// breakDownFocusedSpec pre-fills the chat input with a breakdown directive.
function breakDownFocusedSpec() {
  var input = document.getElementById("spec-chat-input");
  if (input) {
    input.value = "Break this into sub-specs";
    input.focus();
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
  if (currentMode === "spec") {
    _applyMode("spec");
  }
  _initSpecChatResize();
});
