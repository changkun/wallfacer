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
  if (!_focusedSpecPath || !_focusedSpecWorkspace) return;

  // The spec tree returns paths relative to specs/ (e.g., "local/foo.md").
  // The explorer file API expects an absolute path within the workspace.
  var absPath = _focusedSpecWorkspace + "/specs/" + _focusedSpecPath;
  var url =
    Routes.explorer.readFile() +
    "?path=" +
    encodeURIComponent(absPath) +
    "&workspace=" +
    encodeURIComponent(_focusedSpecWorkspace);

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
        // Render mermaid diagrams in code blocks.
        if (typeof renderMermaidBlocks === "function") {
          renderMermaidBlocks(bodyEl);
        }
        // Intercept clicks on links to .md files and navigate within spec mode.
        bodyEl.addEventListener("click", _onSpecBodyLinkClick);
        // Build table of contents from rendered headings.
        _buildSpecToc(bodyEl);
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
      var bodyEl = document.getElementById("spec-focused-body");
      if (bodyEl) bodyEl.textContent = "Error loading spec: " + err.message;
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
function _buildSpecToc(bodyEl) {
  // Remove existing TOC.
  var existing = document.getElementById("spec-toc");
  if (existing) existing.remove();

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
}

// _onSpecBodyLinkClick intercepts clicks on markdown links to .md files
// and navigates within spec mode instead of following the link.
function _onSpecBodyLinkClick(e) {
  var target = e.target;
  // Walk up to find the <a> element.
  while (target && target.tagName !== "A") {
    target = target.parentElement;
  }
  if (!target) return;

  var href = target.getAttribute("href");
  if (!href || !href.endsWith(".md")) return;

  e.preventDefault();

  // Resolve relative paths against the current spec's directory.
  var basePath = _focusedSpecPath || "";
  var baseDir = basePath.substring(0, basePath.lastIndexOf("/") + 1);

  // Normalize: resolve "./" and "../" components.
  var resolved = baseDir + href;
  var parts = resolved.split("/");
  var normalized = [];
  for (var i = 0; i < parts.length; i++) {
    if (parts[i] === "..") {
      normalized.pop();
    } else if (parts[i] !== "." && parts[i] !== "") {
      normalized.push(parts[i]);
    }
  }
  var specPath = normalized.join("/");

  focusSpec(specPath, _focusedSpecWorkspace);
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
