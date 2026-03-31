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

  // Switch explorer root (no-op until spec-explorer is wired).
  if (typeof switchExplorerRoot === "function") {
    switchExplorerRoot(mode === "spec" ? "specs" : "workspace");
  }
}

// Restore persisted mode on page load.
document.addEventListener("DOMContentLoaded", function () {
  if (currentMode === "spec") {
    switchMode("spec");
  }
});
