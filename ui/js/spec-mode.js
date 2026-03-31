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

// switchMode toggles between board and spec mode. Updates header tabs,
// swaps main content visibility, and persists the choice.
function switchMode(mode) {
  if (mode === currentMode) return;
  setCurrentMode(mode);

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
  _loadAndRenderSpec();
  _startSpecRefreshPoll();
  // Update hash for deep-linking.
  history.replaceState(null, "", "#spec/" + encodeURIComponent(specPath));
}

function getFocusedSpecPath() {
  return _focusedSpecPath;
}

function _loadAndRenderSpec() {
  if (!_focusedSpecPath || !_focusedSpecWorkspace) return;

  var url =
    Routes.explorer.readFile() +
    "?path=" +
    encodeURIComponent(_focusedSpecPath) +
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
      if (statusEl) statusEl.textContent = parsed.frontmatter.status || "";
      if (bodyEl) bodyEl.innerHTML = renderMarkdown(parsed.body);

      // Show dispatch button only for validated leaf specs (no children).
      if (dispatchBtn) {
        var isValidated = parsed.frontmatter.status === "validated";
        dispatchBtn.classList.toggle("hidden", !isValidated);
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

// Restore persisted mode on page load.
document.addEventListener("DOMContentLoaded", function () {
  if (currentMode === "spec") {
    switchMode("spec");
  }
  _initSpecChatResize();
});
