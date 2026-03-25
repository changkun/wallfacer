// --- System Prompt Templates ---

var _systemPromptsData = []; // [{name, has_override, content}, ...]
var _systemPromptCurrent = ""; // currently-selected template name

function openSystemPromptsFromSettings(event) {
  if (event && typeof event.preventDefault === "function")
    event.preventDefault();
  if (event && typeof event.stopPropagation === "function")
    event.stopPropagation();
  if (typeof closeSettings === "function") closeSettings();
  showSystemPromptsEditor().catch(function (err) {
    console.error("Failed to open system prompts editor:", err);
  });
}

var _systemPromptsDismiss = null;
async function showSystemPromptsEditor() {
  var modal = document.getElementById("system-prompts-modal");
  if (!modal) return;
  modal.classList.remove("hidden");
  modal.style.display = "flex";
  if (_systemPromptsDismiss) _systemPromptsDismiss();
  _systemPromptsDismiss = bindModalDismiss(modal, closeSystemPromptsEditor);

  // Show prompts dir from config.
  try {
    var cfg = await api("/api/config");
    var dirEl = document.getElementById("system-prompts-dir");
    if (dirEl && cfg.prompts_dir) {
      dirEl.textContent = cfg.prompts_dir;
    }
  } catch (_) {}

  await loadSystemPrompts();
}

function closeSystemPromptsEditor() {
  var modal = document.getElementById("system-prompts-modal");
  if (modal) {
    modal.classList.add("hidden");
    modal.style.display = "";
  }
  _systemPromptsData = [];
  _systemPromptCurrent = "";
  var list = document.getElementById("system-prompts-list");
  if (list) list.innerHTML = "";
  if (_systemPromptsDismiss) {
    _systemPromptsDismiss();
    _systemPromptsDismiss = null;
  }
}

async function loadSystemPrompts() {
  var list = document.getElementById("system-prompts-list");
  if (!list) return;
  try {
    _systemPromptsData = await api("/api/system-prompts");
  } catch (e) {
    list.innerHTML =
      '<div style="font-size:11px;color:var(--text-muted);padding:6px;">Error loading templates: ' +
      escapeHtml(e.message) +
      "</div>";
    return;
  }

  list.innerHTML = "";
  _systemPromptsData.forEach(function (tmpl) {
    var btn = document.createElement("button");
    btn.type = "button";
    btn.dataset.name = tmpl.name;
    btn.style.cssText =
      "display:flex;align-items:center;gap:6px;width:100%;text-align:left;padding:5px 8px;border:1px solid transparent;border-radius:5px;background:none;cursor:pointer;font-size:12px;color:var(--text-secondary);";
    btn.onmouseover = function () {
      if (tmpl.name !== _systemPromptCurrent)
        btn.style.background = "var(--bg-hover,rgba(128,128,128,0.08))";
    };
    btn.onmouseout = function () {
      if (tmpl.name !== _systemPromptCurrent) btn.style.background = "none";
    };

    // Dot indicator for user override.
    var dot = document.createElement("span");
    dot.style.cssText =
      "width:6px;height:6px;border-radius:50%;flex-shrink:0;background:" +
      (tmpl.has_override ? "var(--accent,#d97757)" : "transparent") +
      ";border:1px solid " +
      (tmpl.has_override ? "var(--accent,#d97757)" : "var(--border,#ccc)") +
      ";";
    dot.title = tmpl.has_override
      ? "User override active"
      : "Using embedded default";
    btn.appendChild(dot);

    var label = document.createElement("span");
    label.textContent = tmpl.name.replace(/_/g, "\u200b_"); // zero-width space for wrapping
    label.style.cssText =
      "overflow:hidden;text-overflow:ellipsis;white-space:nowrap;";
    btn.appendChild(label);

    btn.onclick = function () {
      selectSystemPrompt(tmpl.name);
    };
    list.appendChild(btn);
  });

  // Re-select the previously selected template if it still exists.
  if (
    _systemPromptCurrent &&
    _systemPromptsData.some(function (t) {
      return t.name === _systemPromptCurrent;
    })
  ) {
    selectSystemPrompt(_systemPromptCurrent);
  } else if (_systemPromptsData.length > 0) {
    selectSystemPrompt(_systemPromptsData[0].name);
  }
}

function selectSystemPrompt(name) {
  _systemPromptCurrent = name;

  // Highlight selected button.
  var list = document.getElementById("system-prompts-list");
  if (list) {
    Array.from(list.querySelectorAll("button")).forEach(function (btn) {
      var isSelected = btn.dataset.name === name;
      btn.style.background = isSelected
        ? "var(--bg-active,rgba(128,128,128,0.15))"
        : "none";
      btn.style.borderColor = isSelected ? "var(--border)" : "transparent";
    });
  }

  var tmpl = _systemPromptsData.find(function (t) {
    return t.name === name;
  });
  if (!tmpl) return;

  var label = document.getElementById("system-prompt-name-label");
  if (label)
    label.textContent =
      name + (tmpl.has_override ? " (override active)" : " (embedded default)");

  var textarea = document.getElementById("system-prompt-content");
  if (textarea) textarea.value = tmpl.content;
  switchEditTab("sysprompt", "preview");

  var statusEl = document.getElementById("system-prompt-status");
  if (statusEl) statusEl.textContent = "";

  var resetBtn = document.getElementById("system-prompt-reset-btn");
  if (resetBtn) {
    resetBtn.disabled = !tmpl.has_override;
    resetBtn.style.opacity = tmpl.has_override ? "1" : "0.4";
  }
}

async function saveSystemPrompt() {
  if (!_systemPromptCurrent) return;
  var textarea = document.getElementById("system-prompt-content");
  var statusEl = document.getElementById("system-prompt-status");
  if (!textarea || !statusEl) return;

  statusEl.textContent = "Saving\u2026";
  statusEl.style.color = "var(--text-muted)";
  try {
    await api(
      "/api/system-prompts/" + encodeURIComponent(_systemPromptCurrent),
      {
        method: "PUT",
        body: JSON.stringify({ content: textarea.value }),
      },
    );
    statusEl.textContent = "Saved.";
    statusEl.style.color = "var(--text-muted)";
    setTimeout(function () {
      if (statusEl) statusEl.textContent = "";
    }, 2000);
    await loadSystemPrompts();
  } catch (e) {
    statusEl.textContent = "Error: " + e.message;
    statusEl.style.color = "var(--color-error,#e53e3e)";
  }
}

async function resetSystemPromptToDefault() {
  if (!_systemPromptCurrent) return;
  var tmpl = _systemPromptsData.find(function (t) {
    return t.name === _systemPromptCurrent;
  });
  if (!tmpl || !tmpl.has_override) return;
  if (
    !(await showConfirm(
      'Reset "' +
        _systemPromptCurrent +
        '" to the embedded default? Your override will be deleted.',
    ))
  )
    return;

  var statusEl = document.getElementById("system-prompt-status");
  if (statusEl) {
    statusEl.textContent = "Resetting\u2026";
    statusEl.style.color = "var(--text-muted)";
  }
  try {
    await api(
      "/api/system-prompts/" + encodeURIComponent(_systemPromptCurrent),
      { method: "DELETE" },
    );
    if (statusEl) {
      statusEl.textContent = "Reset to default.";
      statusEl.style.color = "var(--text-muted)";
    }
    setTimeout(function () {
      if (statusEl) statusEl.textContent = "";
    }, 2000);
    await loadSystemPrompts();
  } catch (e) {
    if (statusEl) {
      statusEl.textContent = "Error: " + e.message;
      statusEl.style.color = "var(--color-error,#e53e3e)";
    }
  }
}

// Close on outside click.
document.addEventListener("click", function (e) {
  var modal = document.getElementById("system-prompts-modal");
  if (!modal || modal.classList.contains("hidden")) return;
  var card = modal.querySelector(".modal-card");
  if (card && !card.contains(e.target)) {
    closeSystemPromptsEditor();
  }
});
