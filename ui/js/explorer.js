// --- Explorer panel ---
// Left side panel that will host the file tree (Task 4).
// This file handles toggle, resize, and localStorage persistence.

var _explorerDefaultWidth = 260;
var _explorerMinWidth = 200;
var _explorerStorageKeyOpen = "wallfacer-explorer-open";
var _explorerStorageKeyWidth = "wallfacer-explorer-width";

function toggleExplorer() {
  var panel = document.getElementById("explorer-panel");
  if (!panel) return;

  var isHidden = panel.style.display === "none";
  panel.style.display = isHidden ? "" : "none";
  localStorage.setItem(_explorerStorageKeyOpen, isHidden ? "1" : "0");

  var btn = document.getElementById("explorer-toggle-btn");
  if (btn) btn.setAttribute("aria-expanded", String(isHidden));
}

function _initExplorerResize() {
  var handle = document.getElementById("explorer-resize-handle");
  var panel = document.getElementById("explorer-panel");
  if (!handle || !panel) return;

  // Restore persisted width
  var stored = localStorage.getItem(_explorerStorageKeyWidth);
  if (stored) {
    var w = parseInt(stored, 10);
    if (w >= _explorerMinWidth) {
      panel.style.width = w + "px";
    }
  }

  var startX = 0;
  var startW = 0;

  function maxWidth() {
    return Math.floor(window.innerWidth * 0.5);
  }

  function onMouseMove(e) {
    var delta = e.clientX - startX;
    var newW = Math.min(
      maxWidth(),
      Math.max(_explorerMinWidth, startW + delta),
    );
    panel.style.width = newW + "px";
  }

  function onMouseUp() {
    document.removeEventListener("mousemove", onMouseMove);
    document.removeEventListener("mouseup", onMouseUp);
    handle.classList.remove("explorer-panel__resize-handle--active");
    document.body.style.userSelect = "";
    document.body.style.cursor = "";
    localStorage.setItem(
      _explorerStorageKeyWidth,
      parseInt(panel.style.width, 10),
    );
  }

  handle.addEventListener("mousedown", function (e) {
    e.preventDefault();
    startX = e.clientX;
    startW = panel.offsetWidth;
    handle.classList.add("explorer-panel__resize-handle--active");
    document.body.style.userSelect = "none";
    document.body.style.cursor = "col-resize";
    document.addEventListener("mousemove", onMouseMove);
    document.addEventListener("mouseup", onMouseUp);
  });

  // Double-click resets to default width
  handle.addEventListener("dblclick", function () {
    panel.style.width = _explorerDefaultWidth + "px";
    localStorage.setItem(_explorerStorageKeyWidth, _explorerDefaultWidth);
  });
}

function _initExplorer() {
  // Restore open/closed state
  var panel = document.getElementById("explorer-panel");
  if (!panel) return;

  var wasOpen = localStorage.getItem(_explorerStorageKeyOpen);
  if (wasOpen === "1") {
    panel.style.display = "";
    var btn = document.getElementById("explorer-toggle-btn");
    if (btn) btn.setAttribute("aria-expanded", "true");
  }

  _initExplorerResize();
}

// Expose globally
window.toggleExplorer = toggleExplorer;

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", _initExplorer);
} else {
  _initExplorer();
}
