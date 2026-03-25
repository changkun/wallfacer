// --- Prompt template picker and manager ---

var _templatesPickerEl = null;
var _templatesPickerCleanup = null;

// openTemplatesPicker(onSelect) fetches GET /api/templates and renders a
// scrollable, searchable dropdown anchored below the prompt textarea.
// Clicking a row calls onSelect(template.body) and closes the picker.
async function openTemplatesPicker(onSelect) {
  closeTemplatesPicker();

  var textarea = document.getElementById("new-prompt");
  if (!textarea) return;

  var templates = [];
  try {
    templates = await api("/api/templates");
  } catch (e) {
    templates = [];
  }

  // Build dropdown element.
  var el = document.createElement("div");
  el.id = "templates-picker";
  el.style.cssText = [
    "position:absolute",
    "z-index:200",
    "background:var(--bg-card)",
    "border:1px solid var(--border)",
    "border-radius:8px",
    "box-shadow:0 4px 16px rgba(0,0,0,0.15)",
    "min-width:280px",
    "max-width:480px",
    "width:100%",
    "max-height:320px",
    "display:flex",
    "flex-direction:column",
  ].join(";");

  // Position below textarea.
  var rect = textarea.getBoundingClientRect();
  el.style.top = rect.bottom + window.scrollY + 4 + "px";
  el.style.left = rect.left + window.scrollX + "px";

  // Search input.
  var searchInput = document.createElement("input");
  searchInput.type = "text";
  searchInput.placeholder = "Search templates\u2026";
  searchInput.className = "field";
  searchInput.style.cssText =
    "margin:8px;font-size:12px;padding:5px 8px;box-sizing:border-box;width:calc(100% - 16px);";
  el.appendChild(searchInput);

  // Scrollable list.
  var listEl = document.createElement("div");
  listEl.style.cssText =
    "overflow-y:auto;flex:1;min-height:0;padding-bottom:4px;";
  el.appendChild(listEl);

  function renderList(filter) {
    var query = (filter || "").toLowerCase();
    var visible = templates.filter(function (t) {
      return (
        !query ||
        t.name.toLowerCase().includes(query) ||
        t.body.toLowerCase().includes(query)
      );
    });
    listEl.innerHTML = "";
    if (visible.length === 0) {
      var empty = document.createElement("div");
      empty.style.cssText =
        "padding:10px 12px;font-size:12px;color:var(--text-muted);";
      empty.textContent =
        templates.length === 0 ? "No templates saved yet." : "No matches.";
      listEl.appendChild(empty);
      return;
    }
    visible.forEach(function (t) {
      var row = document.createElement("div");
      row.style.cssText = [
        "padding:7px 12px",
        "cursor:pointer",
        "border-bottom:1px solid var(--border)",
      ].join(";");
      row.onmouseenter = function () {
        row.style.background = "var(--bg-hover,var(--bg-card))";
      };
      row.onmouseleave = function () {
        row.style.background = "";
      };

      var nameEl = document.createElement("div");
      nameEl.style.cssText =
        "font-size:13px;font-weight:500;color:var(--text-primary);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;";
      nameEl.textContent = t.name;

      var previewEl = document.createElement("div");
      previewEl.style.cssText =
        "font-size:11px;color:var(--text-muted);margin-top:2px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;";
      previewEl.textContent = t.body.replace(/\n/g, " ");

      row.appendChild(nameEl);
      row.appendChild(previewEl);
      row.onclick = function () {
        closeTemplatesPicker();
        if (typeof onSelect === "function") onSelect(t.body);
      };
      listEl.appendChild(row);
    });
  }

  renderList("");

  searchInput.addEventListener("input", function () {
    renderList(searchInput.value);
  });

  document.body.appendChild(el);
  _templatesPickerEl = el;
  searchInput.focus();

  // Close on outside click.
  function handleOutside(e) {
    if (
      _templatesPickerEl &&
      !_templatesPickerEl.contains(e.target) &&
      e.target !== textarea
    ) {
      closeTemplatesPicker();
    }
  }
  document.addEventListener("mousedown", handleOutside, { capture: true });

  // Close on Escape.
  function handleKey(e) {
    if (e.key === "Escape") closeTemplatesPicker();
  }
  document.addEventListener("keydown", handleKey);

  _templatesPickerCleanup = function () {
    document.removeEventListener("mousedown", handleOutside, { capture: true });
    document.removeEventListener("keydown", handleKey);
  };
}

function closeTemplatesPicker() {
  if (_templatesPickerEl) {
    _templatesPickerEl.remove();
    _templatesPickerEl = null;
  }
  if (_templatesPickerCleanup) {
    _templatesPickerCleanup();
    _templatesPickerCleanup = null;
  }
}

// _ensureTemplatesManagerModal lazily injects the manager modal into the DOM.
function _ensureTemplatesManagerModal() {
  if (document.getElementById("templates-manager-modal")) return;

  var html = [
    '<div id="templates-manager-modal" class="modal-overlay fixed inset-0 z-50 hidden items-center justify-center p-4">',
    '  <div class="modal-card" style="max-width:600px;width:100%;max-height:90vh;display:flex;flex-direction:column;">',
    '    <div class="p-6" style="display:flex;flex-direction:column;flex:1;min-height:0;">',
    '      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:16px;">',
    '        <h3 style="font-size:16px;font-weight:600;margin:0;">Prompt Templates</h3>',
    '        <button onclick="closeTemplatesManager()" style="background:none;border:none;cursor:pointer;font-size:20px;color:var(--text-muted);line-height:1;">&times;</button>',
    "      </div>",
    "      <!-- Add form -->",
    '      <div id="tmpl-add-form" style="border:1px solid var(--border);border-radius:8px;padding:12px;margin-bottom:16px;">',
    '        <div style="font-size:11px;font-weight:600;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;margin-bottom:8px;">Add Template</div>',
    '        <input id="tmpl-new-name" type="text" placeholder="Name\u2026" class="field" style="font-size:12px;padding:5px 8px;margin-bottom:6px;width:100%;box-sizing:border-box;">',
    '        <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:4px;">',
    '          <span class="text-xs text-v-muted">Body</span>',
    '          <div id="tmpl-body-tabs" class="logs-tabs">',
    "            <button class=\"logs-tab active\" onclick=\"switchEditTab('tmplbody','edit')\">Edit</button>",
    "            <button class=\"logs-tab\" onclick=\"switchEditTab('tmplbody','preview')\">Preview</button>",
    "          </div>",
    "        </div>",
    '        <textarea id="tmpl-new-body" rows="4" placeholder="Prompt body\u2026" class="field editable-field" style="font-size:12px;padding:5px 8px;width:100%;box-sizing:border-box;resize:vertical;"></textarea>',
    '        <div id="tmpl-new-body-preview" class="code-block prose-content hidden editable-preview" style="font-size:12px;"></div>',
    '        <div style="display:flex;align-items:center;gap:8px;margin-top:8px;">',
    '          <button onclick="saveNewTemplate()" class="btn btn-accent" style="font-size:12px;">Save</button>',
    '          <span id="tmpl-add-status" style="font-size:11px;color:var(--text-muted);"></span>',
    "        </div>",
    "      </div>",
    "      <!-- Existing templates list -->",
    '      <div id="tmpl-list" style="overflow-y:auto;flex:1;min-height:0;"></div>',
    "    </div>",
    "  </div>",
    "</div>",
  ].join("\n");

  var wrapper = document.createElement("div");
  wrapper.innerHTML = html;
  document.body.appendChild(wrapper.firstElementChild);

  // Close on outside click.
  document.addEventListener("click", function (e) {
    var modal = document.getElementById("templates-manager-modal");
    if (!modal || modal.classList.contains("hidden")) return;
    var card = modal.querySelector(".modal-card");
    if (card && !card.contains(e.target)) closeTemplatesManager();
  });
}

async function openTemplatesManager() {
  _ensureTemplatesManagerModal();
  registerEditTabSection("tmplbody", "tmpl-body-tabs", "tmpl-new-body");
  var modal = document.getElementById("templates-manager-modal");
  modal.classList.remove("hidden");
  modal.style.display = "flex";

  // Clear add form and default to edit mode for new template body.
  var nameInput = document.getElementById("tmpl-new-name");
  var bodyInput = document.getElementById("tmpl-new-body");
  var statusEl = document.getElementById("tmpl-add-status");
  if (nameInput) nameInput.value = "";
  if (bodyInput) bodyInput.value = "";
  if (statusEl) statusEl.textContent = "";
  switchEditTab("tmplbody", "edit");

  await _refreshTemplatesList();
}

function openTemplatesManagerFromSettings(event) {
  var activeEvent =
    event || (typeof window !== "undefined" ? window.event : null);

  if (activeEvent && typeof activeEvent.preventDefault === "function") {
    activeEvent.preventDefault();
  }
  if (activeEvent && typeof activeEvent.stopPropagation === "function") {
    activeEvent.stopPropagation();
  }

  if (typeof closeSettings === "function") {
    closeSettings();
  }

  openTemplatesManager().catch(function (err) {
    showAlert(
      "Failed to open Templates: " +
        (err && err.message ? err.message : "Unknown error"),
    );
    console.error("Failed to open templates manager:", err);
  });
}

function closeTemplatesManager() {
  var modal = document.getElementById("templates-manager-modal");
  if (!modal) return;
  modal.classList.add("hidden");
  modal.style.display = "";
}

async function _refreshTemplatesList() {
  var listEl = document.getElementById("tmpl-list");
  if (!listEl) return;
  listEl.innerHTML =
    '<div style="font-size:12px;color:var(--text-muted);padding:8px 0;">Loading\u2026</div>';

  var templates = [];
  try {
    templates = await api("/api/templates");
  } catch (e) {
    listEl.innerHTML =
      '<div style="font-size:12px;color:var(--text-muted);padding:8px 0;">Error loading templates.</div>';
    return;
  }

  if (templates.length === 0) {
    listEl.innerHTML =
      '<div style="font-size:12px;color:var(--text-muted);padding:8px 0;">No templates yet. Add one above.</div>';
    return;
  }

  listEl.innerHTML = "";
  templates.forEach(function (t) {
    var row = document.createElement("div");
    row.style.cssText =
      "display:flex;align-items:flex-start;gap:10px;padding:10px 0;border-bottom:1px solid var(--border);";

    var info = document.createElement("div");
    info.style.cssText = "flex:1;min-width:0;";

    var nameEl = document.createElement("div");
    nameEl.style.cssText =
      "font-size:13px;font-weight:500;color:var(--text-primary);";
    nameEl.textContent = t.name;

    var previewEl = document.createElement("div");
    previewEl.style.cssText =
      "font-size:11px;color:var(--text-muted);margin-top:3px;white-space:pre-wrap;word-break:break-word;max-height:48px;overflow:hidden;";
    previewEl.textContent = t.body;

    info.appendChild(nameEl);
    info.appendChild(previewEl);

    var delBtn = document.createElement("button");
    delBtn.className = "btn-icon";
    delBtn.style.cssText =
      "font-size:11px;padding:3px 8px;flex-shrink:0;color:var(--text-muted);";
    delBtn.textContent = "Delete";
    delBtn.onclick = function () {
      _deleteTemplate(t.id);
    };

    row.appendChild(info);
    row.appendChild(delBtn);
    listEl.appendChild(row);
  });
}

async function saveNewTemplate() {
  var nameInput = document.getElementById("tmpl-new-name");
  var bodyInput = document.getElementById("tmpl-new-body");
  var statusEl = document.getElementById("tmpl-add-status");
  if (!nameInput || !bodyInput) return;

  var name = nameInput.value.trim();
  var body = bodyInput.value.trim();
  if (!name || !body) {
    if (statusEl) {
      statusEl.textContent = "Name and body are required.";
    }
    return;
  }

  if (statusEl) statusEl.textContent = "Saving\u2026";
  try {
    await api("/api/templates", {
      method: "POST",
      body: JSON.stringify({ name: name, body: body }),
    });
    nameInput.value = "";
    bodyInput.value = "";
    if (statusEl) {
      statusEl.textContent = "Saved.";
      setTimeout(function () {
        statusEl.textContent = "";
      }, 2000);
    }
    await _refreshTemplatesList();
  } catch (e) {
    if (statusEl) statusEl.textContent = "Error: " + e.message;
  }
}

async function _deleteTemplate(id) {
  if (!(await showConfirm("Delete this template?"))) return;
  try {
    await api("/api/templates/" + id, { method: "DELETE" });
    await _refreshTemplatesList();
  } catch (e) {
    showAlert("Error deleting template: " + e.message);
  }
}
