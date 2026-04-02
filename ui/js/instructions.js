// --- Workspace AGENTS.md (Instructions) ---

var _instructionsCtrl = createModalController("instructions-modal");

async function showInstructionsEditor(event, preloadedContent) {
  if (event) event.stopPropagation();
  closeSettings();

  _instructionsCtrl.open();

  var textarea = document.getElementById("instructions-content");
  var pathEl = document.getElementById("instructions-path");
  var statusEl = document.getElementById("instructions-status");
  textarea.value = preloadedContent != null ? preloadedContent : "";
  pathEl.textContent = "";

  if (preloadedContent != null) {
    statusEl.textContent = "Re-initialized.";
    setTimeout(function () {
      statusEl.textContent = "";
    }, 2000);
  } else {
    statusEl.textContent = "Loading\u2026";
  }

  try {
    var config = await api("/api/config");
    if (config.instructions_path) {
      pathEl.textContent = config.instructions_path;
    }
  } catch (e) {
    /* non-critical */
  }

  if (preloadedContent != null) {
    switchEditTab("instructions", "preview");
    return;
  }

  try {
    var data = await api("/api/instructions");
    textarea.value = data.content || "";
    statusEl.textContent = "";
    switchEditTab("instructions", "preview");
  } catch (e) {
    statusEl.textContent = "Error loading: " + e.message;
  }
}

function closeInstructionsEditor() {
  _instructionsCtrl.close();
}

async function saveInstructions() {
  var content = document.getElementById("instructions-content").value;
  var statusEl = document.getElementById("instructions-status");
  statusEl.textContent = "Saving\u2026";
  try {
    await api("/api/instructions", {
      method: "PUT",
      body: JSON.stringify({ content: content }),
    });
    statusEl.textContent = "Saved.";
    setTimeout(function () {
      statusEl.textContent = "";
    }, 2000);
  } catch (e) {
    statusEl.textContent = "Error: " + e.message;
  }
}

// Called from the Re-init button inside the editor modal.
async function reinitInstructionsFromEditor() {
  if (
    !(await showConfirm(
      "Re-initialize from the default template and each repository's AGENTS.md (or legacy CLAUDE.md)? This will overwrite your current edits.",
    ))
  ) {
    return;
  }
  var statusEl = document.getElementById("instructions-status");
  if (statusEl) statusEl.textContent = "Re-initializing\u2026";
  try {
    var data = await api("/api/instructions/reinit", { method: "POST" });
    var textarea = document.getElementById("instructions-content");
    if (textarea) textarea.value = data.content || "";
    if (statusEl) {
      statusEl.textContent = "Re-initialized.";
      setTimeout(function () {
        statusEl.textContent = "";
      }, 2000);
    }
  } catch (e) {
    if (statusEl) statusEl.textContent = "Error: " + e.message;
  }
}
