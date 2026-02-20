// --- Workspace CLAUDE.md (Instructions) ---

async function showInstructionsEditor(event, preloadedContent) {
  // Stop propagation so the document click listener (which closes the modal on
  // outside-click) doesn't fire for the same click that opened the modal.
  if (event) event.stopPropagation();

  // Close settings panel before opening editor.
  document.getElementById('settings-panel').classList.add('hidden');

  var modal = document.getElementById('instructions-modal');
  var textarea = document.getElementById('instructions-content');
  var pathEl = document.getElementById('instructions-path');
  var statusEl = document.getElementById('instructions-status');

  modal.classList.remove('hidden');
  modal.style.display = 'flex';
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

// Called from the settings panel Re-init button (outside the editor).
async function reinitInstructions() {
  if (!confirm('Re-initialize workspace CLAUDE.md from the default template and each repository\'s CLAUDE.md?\n\nThis will overwrite any manual edits.')) {
    return;
  }
  await _doReinit();
}

// Called from the Re-init button inside the editor modal.
async function reinitInstructionsFromEditor() {
  if (!confirm('Re-initialize from the default template and each repository\'s CLAUDE.md?\n\nThis will overwrite your current edits.')) {
    return;
  }
  await _doReinit(true);
}

async function _doReinit(updateEditor) {
  var statusEl = document.getElementById('instructions-status');
  if (updateEditor && statusEl) statusEl.textContent = 'Re-initializing\u2026';
  try {
    var data = await api('/api/instructions/reinit', { method: 'POST' });
    if (updateEditor) {
      var textarea = document.getElementById('instructions-content');
      if (textarea) textarea.value = data.content || '';
      if (statusEl) {
        statusEl.textContent = 'Re-initialized.';
        setTimeout(function() { statusEl.textContent = ''; }, 2000);
      }
    } else {
      // Called from settings panel: open editor so user sees the result.
      showInstructionsEditor(null, data.content || '');
    }
  } catch (e) {
    if (updateEditor && statusEl) {
      statusEl.textContent = 'Error: ' + e.message;
    } else {
      showAlert('Re-init failed: ' + e.message);
    }
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
