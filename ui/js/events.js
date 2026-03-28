// --- Event listeners ---

// Close modal when clicking the overlay backdrop
document.getElementById("modal").addEventListener("click", (e) => {
  if (e.target === document.getElementById("modal")) closeModal();
});

// Close modal on Escape key
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape") {
    if (
      closeFirstVisibleModal([
        { id: "explorer-preview-backdrop", close: closeExplorerPreview },
        { id: "alert-modal", close: closeAlert },
        { id: "stats-modal", close: closeStatsModal },
        { id: "usage-stats-modal", close: closeUsageStats },
        { id: "container-monitor-modal", close: closeContainerMonitor },
        { id: "instructions-modal", close: closeInstructionsEditor },
        { id: "settings-modal", close: closeSettings },
        { id: "keyboard-shortcuts-modal", close: closeKeyboardShortcuts },
        { id: "modal", close: closeModal },
      ])
    )
      return;
  }
});

// Close alert modal when clicking the overlay backdrop
document.getElementById("alert-modal").addEventListener("click", (e) => {
  if (e.target === document.getElementById("alert-modal")) closeAlert();
});

// Global shortcut: "n" opens the new task form, "?" opens keyboard shortcuts
document.addEventListener("keydown", (e) => {
  if (e.ctrlKey || e.metaKey || e.altKey) return;
  if (e.key !== "n" && e.key !== "?" && e.key !== "e") return;
  var tag = document.activeElement && document.activeElement.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
  var ce =
    document.activeElement &&
    document.activeElement.getAttribute("contenteditable");
  if (ce !== null && ce !== "false") return;
  // Don't open if any modal is visible
  var modals = [
    "modal",
    "alert-modal",
    "stats-modal",
    "usage-stats-modal",
    "container-monitor-modal",
    "instructions-modal",
    "settings-modal",
    "keyboard-shortcuts-modal",
  ];
  for (var i = 0; i < modals.length; i++) {
    var m = document.getElementById(modals[i]);
    if (m && !m.classList.contains("hidden")) return;
  }
  e.preventDefault();
  if (e.key === "n") showNewTaskForm();
  if (e.key === "?") openKeyboardShortcuts();
  if (e.key === "e") toggleExplorer();
});

// New task textarea: Ctrl/Cmd+Enter to save, Escape to cancel
document.getElementById("new-prompt").addEventListener("keydown", (e) => {
  if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) {
    e.preventDefault();
    createTask();
  }
  if (e.key === "Escape") {
    e.preventDefault();
    hideNewTaskForm();
  }
});

// New task textarea: auto-grow height and save draft
document.getElementById("new-prompt").addEventListener("input", (e) => {
  e.target.style.height = "";
  e.target.style.height = e.target.scrollHeight + "px";
  localStorage.setItem("wallfacer-new-task-draft", e.target.value);
});

// --- Initialization ---
try {
  initSortable();
} catch (e) {
  console.error("sortable init:", e);
}
try {
  initTrashBin();
} catch (e) {
  console.error("trash bin init:", e);
}
loadMaxParallel();
loadOversightInterval();
loadAutoPush();
fetchConfig();
