// --- Event listeners ---

// Close modal when clicking the overlay backdrop
document.getElementById('modal').addEventListener('click', (e) => {
  if (e.target === document.getElementById('modal')) closeModal();
});

// Close modal on Escape key
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    if (closeFirstVisibleModal([
      { id: 'alert-modal', close: closeAlert },
      { id: 'stats-modal', close: closeStatsModal },
      { id: 'usage-stats-modal', close: closeUsageStats },
      { id: 'container-monitor-modal', close: closeContainerMonitor },
      { id: 'instructions-modal', close: closeInstructionsEditor },
      { id: 'settings-modal', close: closeSettings },
      { id: 'modal', close: closeModal },
    ])) return;
  }
});

// Close alert modal when clicking the overlay backdrop
document.getElementById('alert-modal').addEventListener('click', (e) => {
  if (e.target === document.getElementById('alert-modal')) closeAlert();
});

// New task textarea: Ctrl/Cmd+Enter to save, Escape to cancel
document.getElementById('new-prompt').addEventListener('keydown', (e) => {
  if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
    e.preventDefault();
    createTask();
  }
  if (e.key === 'Escape') {
    e.preventDefault();
    hideNewTaskForm();
  }
});

// New task textarea: auto-grow height and save draft
document.getElementById('new-prompt').addEventListener('input', (e) => {
  e.target.style.height = '';
  e.target.style.height = e.target.scrollHeight + 'px';
  localStorage.setItem('wallfacer-new-task-draft', e.target.value);
});

// --- Initialization ---
try { initSortable(); } catch (e) { console.error('sortable init:', e); }
try { initTrashBin(); } catch (e) { console.error('trash bin init:', e); }
startGitStream();
startTasksStream();
loadMaxParallel();
loadOversightInterval();
loadAutoPush();
fetchConfig();
