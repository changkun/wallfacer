// --- Workspace AGENTS.md (Instructions) ---

var _instructionsDismiss = null;
async function showInstructionsEditor(event, preloadedContent) {
  if (event) event.stopPropagation();
  closeSettings();

  var modal = document.getElementById('instructions-modal');
  var textarea = document.getElementById('instructions-content');
  var pathEl = document.getElementById('instructions-path');
  var statusEl = document.getElementById('instructions-status');

  modal.classList.remove('hidden');
  modal.style.display = 'flex';
  if (_instructionsDismiss) _instructionsDismiss();
  _instructionsDismiss = bindModalDismiss(modal, closeInstructionsEditor);
  textarea.value = preloadedContent != null ? preloadedContent : '';
  pathEl.textContent = '';

  if (preloadedContent != null) {
    statusEl.textContent = 'Re-initialized.';
    setTimeout(function() { statusEl.textContent = ''; }, 2000);
  } else {
    statusEl.textContent = 'Loading\u2026';
  }

  try {
    var config = await api('/api/config');
    if (config.instructions_path) {
      pathEl.textContent = config.instructions_path;
    }
  } catch (e) { /* non-critical */ }

  if (preloadedContent != null) return;

  try {
    var data = await api('/api/instructions');
    textarea.value = data.content || '';
    statusEl.textContent = '';
  } catch (e) {
    statusEl.textContent = 'Error loading: ' + e.message;
  }
}

function closeInstructionsEditor() {
  var modal = document.getElementById('instructions-modal');
  modal.classList.add('hidden');
  modal.style.display = '';
  if (_instructionsDismiss) { _instructionsDismiss(); _instructionsDismiss = null; }
}

async function saveInstructions() {
  var content = document.getElementById('instructions-content').value;
  var statusEl = document.getElementById('instructions-status');
  statusEl.textContent = 'Saving\u2026';
  try {
    await api('/api/instructions', {
      method: 'PUT',
      body: JSON.stringify({ content: content }),
    });
    statusEl.textContent = 'Saved.';
    setTimeout(function() { statusEl.textContent = ''; }, 2000);
  } catch (e) {
    statusEl.textContent = 'Error: ' + e.message;
  }
}

// Called from the Re-init button inside the editor modal.
async function reinitInstructionsFromEditor() {
  if (!confirm('Re-initialize from the default template and each repository\'s AGENTS.md (or legacy CLAUDE.md)?\n\nThis will overwrite your current edits.')) {
    return;
  }
  var statusEl = document.getElementById('instructions-status');
  if (statusEl) statusEl.textContent = 'Re-initializing\u2026';
  try {
    var data = await api('/api/instructions/reinit', { method: 'POST' });
    var textarea = document.getElementById('instructions-content');
    if (textarea) textarea.value = data.content || '';
    if (statusEl) {
      statusEl.textContent = 'Re-initialized.';
      setTimeout(function() { statusEl.textContent = ''; }, 2000);
    }
  } catch (e) {
    if (statusEl) statusEl.textContent = 'Error: ' + e.message;
  }
}

// Close instructions modal on outside click.
document.addEventListener('click', function(e) {
  var modal = document.getElementById('instructions-modal');
  if (!modal || modal.classList.contains('hidden')) return;
  var card = modal.querySelector('.modal-card');
  if (card && !card.contains(e.target)) {
    closeInstructionsEditor();
  }
});
