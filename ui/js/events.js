// --- Event listeners ---

// Close modal when clicking the overlay backdrop
document.getElementById('modal').addEventListener('click', (e) => {
  if (e.target === document.getElementById('modal')) closeModal();
});

// Close modal on Escape key
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') {
    const alertModal = document.getElementById('alert-modal');
    if (!alertModal.classList.contains('hidden')) { closeAlert(); return; }
    const containerModal = document.getElementById('container-monitor-modal');
    if (!containerModal.classList.contains('hidden')) { closeContainerMonitor(); return; }
    const instructionsModal = document.getElementById('instructions-modal');
    if (instructionsModal && !instructionsModal.classList.contains('hidden')) { closeInstructionsEditor(); return; }
    const settingsModal = document.getElementById('settings-modal');
    if (settingsModal && !settingsModal.classList.contains('hidden')) { closeSettings(); return; }
    closeModal();
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
startGitStream();
startTasksStream();
loadMaxParallel();
loadOversightInterval();
fetchConfig();
