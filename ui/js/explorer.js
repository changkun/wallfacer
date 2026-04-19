// --- Explorer panel ---
// Left side panel hosting a lazy-loading directory tree.
// Task 3: toggle, resize, localStorage persistence.
// Task 4: tree fetching, rendering, keyboard navigation.

var _explorerDefaultWidth = 260;
var _explorerMinWidth = 200;
var _explorerStorageKeyOpen = "wallfacer-explorer-open";
var _explorerStorageKeyWidth = "wallfacer-explorer-width";

// --- Tree state ---
var _explorerRoots = [];
var _explorerLoaded = false;
var _explorerRefreshTimer = null;
var _explorerStreamHandle = null;

// --- Task Prompts virtual section state ---
var _taskPrompts = [];
var _taskPromptsIncludeWaiting = false;
var _taskPromptsExpanded = true;
var _taskPromptsStreamHandle = null;

// ---------------------------------------------------------------------------
// Toggle & resize (Task 3)
// ---------------------------------------------------------------------------

function toggleExplorer() {
  var panel = document.getElementById("explorer-panel");
  if (!panel) return;

  var isHidden = panel.style.display === "none";
  panel.style.display = isHidden ? "" : "none";
  localStorage.setItem(_explorerStorageKeyOpen, isHidden ? "1" : "0");

  var btn = document.getElementById("explorer-toggle-btn");
  if (btn) btn.setAttribute("aria-expanded", String(isHidden));

  if (isHidden) {
    // When in spec mode, delegate to the spec explorer instead of loading
    // workspace files. This prevents the workspace poll from overwriting
    // the spec tree.
    if (typeof getCurrentMode === "function" && getCurrentMode() === "spec") {
      if (typeof switchExplorerRoot === "function") {
        switchExplorerRoot("specs");
      }
    } else {
      // Load tree on first open, refresh expanded dirs on subsequent opens.
      if (!_explorerLoaded) {
        _loadExplorerRoots();
      } else {
        _refreshExpandedNodes();
      }
      _startExplorerRefreshPoll();
    }
  } else {
    _stopExplorerRefreshPoll();
    if (typeof _stopSpecTreePoll === "function") {
      _stopSpecTreePoll();
    }
  }
}

function _startExplorerRefreshPoll() {
  _stopExplorerRefreshPoll();
  _startExplorerStream();
  _startTaskPromptsStream();
}

function _stopExplorerRefreshPoll() {
  if (_explorerRefreshTimer) {
    clearInterval(_explorerRefreshTimer);
    _explorerRefreshTimer = null;
  }
  _stopExplorerStream();
  _stopTaskPromptsStream();
}

function _startExplorerStream() {
  _stopExplorerStream();
  if (!Routes || !Routes.explorer || !Routes.explorer.stream) return;

  _explorerStreamHandle = createSSEStream({
    url: Routes.explorer.stream(),
    listeners: {
      refresh: function () {
        if (_explorerLoaded) _refreshExpandedNodes();
      },
      heartbeat: function () {},
    },
  });
}

function _stopExplorerStream() {
  if (_explorerStreamHandle) {
    _explorerStreamHandle.stop();
    _explorerStreamHandle = null;
  }
}

// ---------------------------------------------------------------------------
// Task Prompts virtual section
// ---------------------------------------------------------------------------

function _startTaskPromptsStream() {
  _stopTaskPromptsStream();
  if (!Routes || !Routes.tasks || !Routes.tasks.stream) return;

  _taskPromptsStreamHandle = createSSEStream({
    url: Routes.tasks.stream(),
    listeners: {
      snapshot: function () {
        _loadTaskPrompts();
      },
      "task-updated": function () {
        _loadTaskPrompts();
      },
      "task-deleted": function () {
        _loadTaskPrompts();
      },
      heartbeat: function () {},
    },
  });
}

function _stopTaskPromptsStream() {
  if (_taskPromptsStreamHandle) {
    _taskPromptsStreamHandle.stop();
    _taskPromptsStreamHandle = null;
  }
}

function _loadTaskPrompts() {
  if (!Routes || !Routes.explorer || !Routes.explorer.taskPrompts) return;
  var url = Routes.explorer.taskPrompts();
  if (_taskPromptsIncludeWaiting) {
    url += "?status=backlog,waiting";
  }
  api(url)
    .then(function (data) {
      _taskPrompts = Array.isArray(data) ? data : [];
      _renderTree();
    })
    .catch(function () {});
}

function _toggleTaskPromptsWaiting() {
  _taskPromptsIncludeWaiting = !_taskPromptsIncludeWaiting;
  _loadTaskPrompts();
}

function _renderTaskPromptsSection(container) {
  var section = document.createElement("div");
  section.className = "explorer-task-prompts";

  // Section header
  var header = document.createElement("div");
  header.className = "explorer-task-prompts__header";
  header.setAttribute("tabindex", "0");
  header.setAttribute("role", "button");
  header.setAttribute("aria-expanded", String(_taskPromptsExpanded));

  var chevron = document.createElement("span");
  chevron.className = "explorer-node__toggle";
  chevron.innerHTML = _taskPromptsExpanded ? _chevDownSvg : _chevRightSvg;
  header.appendChild(chevron);

  var label = document.createElement("span");
  label.className = "explorer-task-prompts__label";
  label.textContent = "Task Prompts";
  header.appendChild(label);

  var toggle = document.createElement("button");
  toggle.className = "explorer-task-prompts__waiting-toggle";
  toggle.title = _taskPromptsIncludeWaiting
    ? "Hide waiting tasks"
    : "Show waiting tasks";
  toggle.textContent = _taskPromptsIncludeWaiting ? "W" : "w";
  toggle.setAttribute("aria-pressed", String(_taskPromptsIncludeWaiting));
  toggle.addEventListener("click", function (e) {
    e.stopPropagation();
    _toggleTaskPromptsWaiting();
  });
  header.appendChild(toggle);

  header.addEventListener("click", function () {
    _taskPromptsExpanded = !_taskPromptsExpanded;
    _renderTree();
  });
  header.addEventListener("keydown", function (e) {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      _taskPromptsExpanded = !_taskPromptsExpanded;
      _renderTree();
    }
  });

  section.appendChild(header);

  if (_taskPromptsExpanded) {
    if (_taskPrompts.length === 0) {
      var empty = document.createElement("div");
      empty.className = "explorer-task-prompts__empty";
      empty.textContent = "No tasks";
      section.appendChild(empty);
    } else {
      for (var i = 0; i < _taskPrompts.length; i++) {
        section.appendChild(_renderTaskPromptEntry(_taskPrompts[i]));
      }
    }
  }

  container.appendChild(section);
}

function _renderTaskPromptEntry(entry) {
  var el = document.createElement("div");
  el.className = "explorer-task-prompts__entry";
  el.setAttribute("tabindex", "0");
  el.setAttribute("data-task-id", entry.task_id);

  var badge = document.createElement("span");
  badge.className =
    "explorer-task-prompts__badge explorer-task-prompts__badge--" +
    entry.status;
  badge.textContent = entry.status;
  el.appendChild(badge);

  var title = document.createElement("span");
  title.className = "explorer-task-prompts__title";
  title.textContent = entry.title;
  el.appendChild(title);

  var ts = document.createElement("span");
  ts.className = "explorer-task-prompts__time";
  try {
    var d = new Date(entry.updated_at);
    ts.textContent = d.toLocaleDateString();
  } catch (_e) {}
  el.appendChild(ts);

  el.addEventListener("click", function () {
    if (
      typeof window !== "undefined" &&
      typeof window.openPlanForTask === "function"
    ) {
      window.openPlanForTask(entry.task_id, entry.title, entry.status);
    }
  });
  el.addEventListener("keydown", function (e) {
    if (e.key === "Enter") {
      if (
        typeof window !== "undefined" &&
        typeof window.openPlanForTask === "function"
      ) {
        window.openPlanForTask(entry.task_id, entry.title, entry.status);
      }
    }
  });

  return el;
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

// ---------------------------------------------------------------------------
// Tree data (Task 4)
// ---------------------------------------------------------------------------

function _basename(p) {
  var parts = p.replace(/[/\\]+$/, "").split(/[/\\]/);
  return parts[parts.length - 1] || p;
}

// Build child node objects from API response entries.
// Exported via window for testing.
function _buildChildNodes(entries, parentPath, workspace) {
  var nodes = [];
  for (var i = 0; i < entries.length; i++) {
    var e = entries[i];
    nodes.push({
      path: parentPath + "/" + e.name,
      name: e.name,
      type: e.type,
      workspace: workspace,
      expanded: false,
      children: null,
      loading: false,
    });
  }
  return nodes;
}

function _loadExplorerRoots() {
  var workspaces =
    typeof activeWorkspaces !== "undefined" && Array.isArray(activeWorkspaces)
      ? activeWorkspaces
      : [];

  _explorerRoots = [];
  for (var i = 0; i < workspaces.length; i++) {
    var ws = workspaces[i];
    _explorerRoots.push({
      path: ws,
      name: _basename(ws),
      type: "dir",
      workspace: ws,
      expanded: false,
      children: null,
      loading: false,
    });
  }
  _explorerLoaded = true;
  _loadTaskPrompts();
  _renderTree();
  // Auto-expand root workspace directories so the tree opens by default.
  for (var r = 0; r < _explorerRoots.length; r++) {
    _expandNode(_explorerRoots[r]);
  }
}

function _expandNode(node) {
  node.loading = true;
  _renderTree();

  var url =
    Routes.explorer.tree() +
    "?path=" +
    encodeURIComponent(node.path) +
    "&workspace=" +
    encodeURIComponent(node.workspace);

  api(url)
    .then(function (entries) {
      var newChildren = _buildChildNodes(
        entries || [],
        node.path,
        node.workspace,
      );
      // Preserve expanded state and children of existing child nodes so that
      // the periodic refresh (_refreshExpandedNodes) does not collapse nested
      // directories that were already expanded.
      if (node.children) {
        var oldByPath = {};
        for (var j = 0; j < node.children.length; j++) {
          oldByPath[node.children[j].path] = node.children[j];
        }
        for (var k = 0; k < newChildren.length; k++) {
          var prev = oldByPath[newChildren[k].path];
          if (prev && prev.expanded) {
            newChildren[k].expanded = true;
            newChildren[k].children = prev.children;
          }
        }
      }
      node.children = newChildren;
      node.expanded = true;
      node.loading = false;
      _renderTree();
    })
    .catch(function () {
      node.loading = false;
      _renderTree();
    });
}

// Re-fetch children for every expanded directory so newly created (or
// deleted) files become visible without a full tree reload.  Only refreshes
// root-level expanded nodes; nested expanded state is preserved by the merge
// logic inside _expandNode.
function _refreshExpandedNodes() {
  for (var i = 0; i < _explorerRoots.length; i++) {
    var n = _explorerRoots[i];
    if (n.expanded && n.type === "dir") {
      _expandNode(n);
    }
  }
}

function _collapseNode(node) {
  node.expanded = false;
  node.children = null;
  _renderTree();
}

function _toggleNode(node) {
  if (node.type !== "dir") return;
  if (node.expanded) {
    _collapseNode(node);
  } else {
    _expandNode(node);
  }
}

// ---------------------------------------------------------------------------
// File preview modal (Task 5)
// ---------------------------------------------------------------------------

var _previewFocusReturn = null;
var _previewNode = null; // currently previewed node
var _previewRawContent = null; // raw text content for edit mode
var _editOriginalContent = null; // content when edit started (for dirty detection)
var _editMode = false;

// Classify a file response into {type, content, size, max}.
// Exported via window for testing.
function _classifyFileResponse(status, contentType, body) {
  if (status === 413) {
    var parsed = typeof body === "string" ? JSON.parse(body) : body;
    return { type: "large", size: parsed.size || 0, max: parsed.max || 0 };
  }
  if (contentType && contentType.indexOf("application/json") !== -1) {
    var json = typeof body === "string" ? JSON.parse(body) : body;
    if (json.binary) {
      return { type: "binary", size: json.size || 0 };
    }
  }
  return { type: "text", content: typeof body === "string" ? body : "" };
}

function _formatSize(bytes) {
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB";
  return (bytes / (1024 * 1024)).toFixed(1) + " MB";
}

function _relativePath(fullPath, workspace) {
  if (fullPath.indexOf(workspace) === 0) {
    var rel = fullPath.slice(workspace.length);
    if (rel.charAt(0) === "/" || rel.charAt(0) === "\\") rel = rel.slice(1);
    return rel || _basename(fullPath);
  }
  return fullPath;
}

function _renderHighlightedContent(content, filename) {
  var lang = typeof extToLang === "function" ? extToLang(filename) : null;
  var highlighted = "";
  try {
    if (lang && typeof hljs !== "undefined") {
      highlighted = hljs.highlight(content, { language: lang }).value;
    } else if (typeof hljs !== "undefined") {
      highlighted = hljs.highlightAuto(content).value;
    } else {
      highlighted = escapeHtml(content);
    }
  } catch (_) {
    highlighted = escapeHtml(content);
  }

  var lines =
    typeof splitHighlightedLines === "function"
      ? splitHighlightedLines(highlighted)
      : highlighted.split("\n");

  var html = '<pre class="explorer-preview__code"><code>';
  for (var i = 0; i < lines.length; i++) {
    html +=
      '<span class="explorer-preview__line">' +
      '<span class="explorer-preview__ln">' +
      (i + 1) +
      "</span>" +
      '<span class="explorer-preview__lc">' +
      (lines[i] || " ") +
      "</span>" +
      "</span>";
  }
  html += "</code></pre>";
  return html;
}

function _toggleMarkdownView() {
  var rendered = document.getElementById("explorer-md-rendered");
  var raw = document.getElementById("explorer-md-raw");
  var btn = document.getElementById("explorer-md-toggle-btn");
  if (!rendered || !raw || !btn) return;

  var showingRendered = !rendered.classList.contains("hidden");
  if (showingRendered) {
    rendered.classList.add("hidden");
    raw.classList.remove("hidden");
    btn.textContent = "Preview";
  } else {
    rendered.classList.remove("hidden");
    raw.classList.add("hidden");
    btn.textContent = "Raw";
  }
}

function _openFilePreview(node) {
  _previewFocusReturn = document.activeElement;
  _previewNode = node;
  _previewRawContent = null;
  _editOriginalContent = null;
  _editMode = false;

  // Create or show backdrop
  var backdrop = document.getElementById("explorer-preview-backdrop");
  if (!backdrop) {
    backdrop = document.createElement("div");
    backdrop.id = "explorer-preview-backdrop";
    backdrop.className =
      "explorer-preview-backdrop fixed inset-0 z-50 items-center justify-center p-4";
    backdrop.addEventListener("click", function (e) {
      if (e.target === backdrop) closeExplorerPreview();
    });
    document.body.appendChild(backdrop);
  }
  backdrop.classList.remove("hidden");
  backdrop.style.display = "";

  var relPath = _relativePath(node.path, node.workspace);

  backdrop.innerHTML =
    '<div class="explorer-preview" onclick="event.stopPropagation()">' +
    '<div class="explorer-preview__header">' +
    '<span class="explorer-preview__path">' +
    escapeHtml(relPath) +
    "</span>" +
    '<div class="explorer-preview__actions">' +
    '<button id="explorer-md-toggle-btn" class="explorer-preview__edit-btn" onclick="_toggleMarkdownView()" style="display:none">Raw</button>' +
    '<button id="explorer-edit-btn" class="explorer-preview__edit-btn" onclick="_enterEditMode()" style="display:none">Edit</button>' +
    '<button id="explorer-save-btn" class="explorer-preview__save-btn" onclick="_saveFile()" style="display:none">Save</button>' +
    '<button id="explorer-discard-btn" class="explorer-preview__discard-btn" onclick="_discardEdit()" style="display:none">Discard</button>' +
    '<button class="explorer-preview__close" onclick="closeExplorerPreview()">&times;</button>' +
    "</div>" +
    "</div>" +
    '<div class="explorer-preview__content">' +
    '<div class="explorer-preview__placeholder">Loading\u2026</div>' +
    "</div>" +
    '<div id="explorer-edit-error" class="explorer-preview__error" style="display:none"></div>' +
    "</div>";

  var contentEl = backdrop.querySelector(".explorer-preview__content");

  var url =
    Routes.explorer.readFile() +
    "?path=" +
    encodeURIComponent(node.path) +
    "&workspace=" +
    encodeURIComponent(node.workspace);

  fetch(url)
    .then(function (res) {
      var ct = res.headers.get("content-type") || "";
      if (res.status === 413) {
        return res.text().then(function (body) {
          return _classifyFileResponse(413, ct, body);
        });
      }
      if (!res.ok) {
        return res.text().then(function (text) {
          throw new Error(text || "Failed to load file");
        });
      }
      if (ct.indexOf("application/json") !== -1) {
        return res.json().then(function (json) {
          return _classifyFileResponse(res.status, ct, json);
        });
      }
      return res.text().then(function (text) {
        return _classifyFileResponse(res.status, ct, text);
      });
    })
    .then(function (result) {
      if (!contentEl) return;

      if (result.type === "large") {
        contentEl.innerHTML =
          '<div class="explorer-preview__placeholder">File too large to preview (' +
          _formatSize(result.size) +
          ", max " +
          _formatSize(result.max) +
          ").</div>";
        return;
      }

      if (result.type === "binary") {
        contentEl.innerHTML =
          '<div class="explorer-preview__placeholder">Binary file (' +
          _formatSize(result.size) +
          ")</div>";
        return;
      }

      // Text file — store raw content and show Edit button
      _previewRawContent = result.content;
      var editBtn = document.getElementById("explorer-edit-btn");
      if (editBtn) editBtn.style.display = "";

      // Markdown files: show rendered view by default with Raw toggle
      var lowerName = node.name.toLowerCase();
      var isMd = lowerName.endsWith(".md") || lowerName.endsWith(".mdx");
      if (isMd && typeof renderMarkdown === "function") {
        var mdToggle = document.getElementById("explorer-md-toggle-btn");
        if (mdToggle) mdToggle.style.display = "";

        contentEl.innerHTML =
          '<div id="explorer-md-rendered" class="explorer-preview__markdown prose-content">' +
          renderMarkdown(result.content) +
          "</div>" +
          '<div id="explorer-md-raw" class="hidden">' +
          _renderHighlightedContent(result.content, node.name) +
          "</div>";
        var mdRendered = document.getElementById("explorer-md-rendered");
        if (mdRendered) _mdRender.enhanceMarkdown(mdRendered);
      } else {
        contentEl.innerHTML = _renderHighlightedContent(
          result.content,
          node.name,
        );
      }
    })
    .catch(function (err) {
      if (contentEl) {
        contentEl.innerHTML =
          '<div class="explorer-preview__placeholder">' +
          escapeHtml(err.message || "Failed to load file") +
          "</div>";
      }
    });
}

// ---------------------------------------------------------------------------
// Edit mode (Task 9)
// ---------------------------------------------------------------------------

function _isEditDirty() {
  if (!_editMode) return false;
  var ta = document.getElementById("explorer-edit-textarea");
  if (!ta) return false;
  return ta.value !== _editOriginalContent;
}

function _enterEditMode() {
  _editMode = true;
  _editOriginalContent = _previewRawContent;

  var backdrop = document.getElementById("explorer-preview-backdrop");
  if (!backdrop) return;

  var contentEl = backdrop.querySelector(".explorer-preview__content");
  if (contentEl) {
    contentEl.innerHTML =
      '<textarea id="explorer-edit-textarea" class="explorer-preview__textarea" spellcheck="false" autocomplete="off"></textarea>';
    var ta = document.getElementById("explorer-edit-textarea");
    if (ta) {
      ta.value = _previewRawContent || "";
      ta.addEventListener("keydown", function (e) {
        if (e.key === "Tab" && !e.shiftKey) {
          e.preventDefault();
          var start = ta.selectionStart;
          var end = ta.selectionEnd;
          ta.value =
            ta.value.substring(0, start) + "\t" + ta.value.substring(end);
          ta.selectionStart = ta.selectionEnd = start + 1;
        }
      });
      ta.focus();
    }
  }

  // Toggle button visibility
  var editBtn = document.getElementById("explorer-edit-btn");
  var saveBtn = document.getElementById("explorer-save-btn");
  var discardBtn = document.getElementById("explorer-discard-btn");
  if (editBtn) editBtn.style.display = "none";
  if (saveBtn) saveBtn.style.display = "";
  if (discardBtn) discardBtn.style.display = "";

  // Clear any previous error
  var errEl = document.getElementById("explorer-edit-error");
  if (errEl) errEl.style.display = "none";
}

function _saveFile() {
  var ta = document.getElementById("explorer-edit-textarea");
  var saveBtn = document.getElementById("explorer-save-btn");
  var errEl = document.getElementById("explorer-edit-error");
  if (!ta || !_previewNode) return;

  var content = ta.value;

  // Loading state
  if (saveBtn) {
    saveBtn.disabled = true;
    saveBtn.textContent = "Saving\u2026";
  }
  if (errEl) errEl.style.display = "none";

  api(Routes.explorer.writeFile(), {
    method: "PUT",
    body: JSON.stringify({
      path: _previewNode.path,
      workspace: _previewNode.workspace,
      content: content,
    }),
  })
    .then(function () {
      // Update stored content and exit edit mode
      _previewRawContent = content;
      _editOriginalContent = null;
      _editMode = false;

      // Re-render preview with updated content
      var backdrop = document.getElementById("explorer-preview-backdrop");
      if (backdrop) {
        var contentEl = backdrop.querySelector(".explorer-preview__content");
        if (contentEl) {
          contentEl.innerHTML = _renderHighlightedContent(
            content,
            _previewNode.name,
          );
        }
      }

      // Toggle buttons back
      var editBtn = document.getElementById("explorer-edit-btn");
      var discardBtn = document.getElementById("explorer-discard-btn");
      if (editBtn) editBtn.style.display = "";
      if (saveBtn) {
        saveBtn.style.display = "none";
        saveBtn.disabled = false;
        saveBtn.textContent = "Save";
      }
      if (discardBtn) discardBtn.style.display = "none";
    })
    .catch(function (err) {
      if (errEl) {
        errEl.textContent = err.message || "Failed to save file";
        errEl.style.display = "";
      }
      if (saveBtn) {
        saveBtn.disabled = false;
        saveBtn.textContent = "Save";
      }
    });
}

async function _discardEdit() {
  if (_isEditDirty()) {
    if (!(await showConfirm("You have unsaved changes. Discard?"))) return;
  }

  _editMode = false;
  _editOriginalContent = null;

  // Re-render preview
  var backdrop = document.getElementById("explorer-preview-backdrop");
  if (backdrop) {
    var contentEl = backdrop.querySelector(".explorer-preview__content");
    if (contentEl && _previewNode) {
      contentEl.innerHTML = _renderHighlightedContent(
        _previewRawContent || "",
        _previewNode.name,
      );
    }
  }

  // Toggle buttons
  var editBtn = document.getElementById("explorer-edit-btn");
  var saveBtn = document.getElementById("explorer-save-btn");
  var discardBtn = document.getElementById("explorer-discard-btn");
  if (editBtn) editBtn.style.display = "";
  if (saveBtn) saveBtn.style.display = "none";
  if (discardBtn) discardBtn.style.display = "none";

  var errEl = document.getElementById("explorer-edit-error");
  if (errEl) errEl.style.display = "none";
}

async function closeExplorerPreview() {
  if (_isEditDirty()) {
    if (!(await showConfirm("You have unsaved changes. Discard?"))) return;
  }

  _editMode = false;
  _editOriginalContent = null;
  _previewRawContent = null;
  _previewNode = null;

  var backdrop = document.getElementById("explorer-preview-backdrop");
  if (backdrop) {
    backdrop.classList.add("hidden");
    backdrop.style.display = "none";
  }
  if (_previewFocusReturn && typeof _previewFocusReturn.focus === "function") {
    _previewFocusReturn.focus();
    _previewFocusReturn = null;
  }
}

// Open a file in the explorer preview by its relative path.
// Used by the unified markdown link handler to open code file references.
function openExplorerFile(relPath) {
  var workspaces =
    typeof activeWorkspaces !== "undefined" && Array.isArray(activeWorkspaces)
      ? activeWorkspaces
      : [];
  if (!workspaces.length) return;
  // Default to the first workspace.
  var ws = workspaces[0];
  var fullPath = ws + "/" + relPath;
  var name = relPath.substring(relPath.lastIndexOf("/") + 1) || relPath;
  _openFilePreview({
    path: fullPath,
    name: name,
    type: "file",
    workspace: ws,
  });
}

// ---------------------------------------------------------------------------
// Tree rendering (Task 4)
// ---------------------------------------------------------------------------

// Collect all visible nodes in DFS order (for keyboard navigation).
function _getVisibleNodes(roots) {
  var result = [];
  function walk(nodes) {
    if (!nodes) return;
    for (var i = 0; i < nodes.length; i++) {
      var n = nodes[i];
      result.push(n);
      if (n.expanded && n.children) {
        walk(n.children);
      }
    }
  }
  walk(roots);
  return result;
}

// Find the parent node of a given node by path traversal.
function _findParent(roots, node) {
  function walk(nodes) {
    if (!nodes) return null;
    for (var i = 0; i < nodes.length; i++) {
      if (nodes[i].children) {
        for (var j = 0; j < nodes[i].children.length; j++) {
          if (nodes[i].children[j] === node) return nodes[i];
        }
        var found = walk(nodes[i].children);
        if (found) return found;
      }
    }
    return null;
  }
  return walk(roots);
}

function _renderTree() {
  var container = document.getElementById("explorer-tree");
  if (!container) return;
  container.innerHTML = "";

  container.setAttribute("role", "tree");

  // Render Task Prompts section above workspace roots in workspace mode.
  var inSpecMode =
    typeof getCurrentMode === "function" && getCurrentMode() === "spec";
  if (!inSpecMode) {
    _renderTaskPromptsSection(container);
  }

  for (var i = 0; i < _explorerRoots.length; i++) {
    _renderNode(_explorerRoots[i], 0, container);
  }
}

// ---------------------------------------------------------------------------
// File icons (Task 11)
// ---------------------------------------------------------------------------

var _iconSvgAttrs =
  'width="14" height="14" viewBox="0 0 24 24" fill="none" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"';

// Disclosure chevrons matching the reference design.
var _chevRightSvg =
  '<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="9 18 15 12 9 6"></polyline></svg>';
var _chevDownSvg =
  '<svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"></polyline></svg>';

function _svgWrap(color, paths) {
  return (
    "<svg " + _iconSvgAttrs + ' stroke="' + color + '">' + paths + "</svg>"
  );
}

// Standard path fragments
var _pFolder =
  '<path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path>';
var _pFolderOpen =
  '<path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path><line x1="9" y1="9" x2="9" y2="21"></line>';
var _pFile =
  '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><polyline points="14 2 14 8 20 8"></polyline>';
var _pGear =
  '<circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09a1.65 1.65 0 0 0-1.08-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09a1.65 1.65 0 0 0 1.51-1.08 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"></path>';
var _pImage =
  '<rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect><circle cx="8.5" cy="8.5" r="1.5"></circle><polyline points="21 15 16 10 5 21"></polyline>';
var _pDatabase =
  '<ellipse cx="12" cy="5" rx="9" ry="3"></ellipse><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"></path><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"></path>';

var _extIconMap = {
  go: function () {
    return _svgWrap("#00ADD8", _pFile);
  },
  js: function () {
    return _svgWrap("#F0DB4F", _pFile);
  },
  mjs: function () {
    return _svgWrap("#F0DB4F", _pFile);
  },
  cjs: function () {
    return _svgWrap("#F0DB4F", _pFile);
  },
  jsx: function () {
    return _svgWrap("#61DAFB", _pFile);
  },
  ts: function () {
    return _svgWrap("#3178C6", _pFile);
  },
  tsx: function () {
    return _svgWrap("#3178C6", _pFile);
  },
  css: function () {
    return _svgWrap("#A86EDB", _pFile);
  },
  scss: function () {
    return _svgWrap("#C76494", _pFile);
  },
  html: function () {
    return _svgWrap("#E44D26", _pFile);
  },
  htm: function () {
    return _svgWrap("#E44D26", _pFile);
  },
  json: function () {
    return _svgWrap("#A0B840", _pFile);
  },
  md: function () {
    return _svgWrap("#6CB6FF", _pFile);
  },
  mdx: function () {
    return _svgWrap("#6CB6FF", _pFile);
  },
  yaml: function () {
    return _svgWrap("#CB4B60", _pFile);
  },
  yml: function () {
    return _svgWrap("#CB4B60", _pFile);
  },
  py: function () {
    return _svgWrap("#3776AB", _pFile);
  },
  pyi: function () {
    return _svgWrap("#3776AB", _pFile);
  },
  rs: function () {
    return _svgWrap("#C4623F", _pFile);
  },
  sh: function () {
    return _svgWrap("#4EAA25", _pFile);
  },
  bash: function () {
    return _svgWrap("#4EAA25", _pFile);
  },
  zsh: function () {
    return _svgWrap("#4EAA25", _pFile);
  },
  sql: function () {
    return _svgWrap("#E8A838", _pDatabase);
  },
  env: function () {
    return _svgWrap("var(--text-muted)", _pGear);
  },
  toml: function () {
    return _svgWrap("var(--text-muted)", _pGear);
  },
  ini: function () {
    return _svgWrap("var(--text-muted)", _pGear);
  },
  cfg: function () {
    return _svgWrap("var(--text-muted)", _pGear);
  },
  png: function () {
    return _svgWrap("#4EAA86", _pImage);
  },
  jpg: function () {
    return _svgWrap("#4EAA86", _pImage);
  },
  jpeg: function () {
    return _svgWrap("#4EAA86", _pImage);
  },
  gif: function () {
    return _svgWrap("#C060C0", _pImage);
  },
  svg: function () {
    return _svgWrap("#E44D26", _pImage);
  },
  webp: function () {
    return _svgWrap("#4EAA86", _pImage);
  },
  ico: function () {
    return _svgWrap("#4EAA86", _pImage);
  },
  txt: function () {
    return _svgWrap("var(--text-muted)", _pFile);
  },
  log: function () {
    return _svgWrap("var(--text-muted)", _pFile);
  },
};

// Special full-filename matches (case-insensitive)
var _nameIconMap = {
  makefile: function () {
    return _svgWrap("#6D8C2E", _pGear);
  },
  dockerfile: function () {
    return _svgWrap("#2496ED", _pFile);
  },
  license: function () {
    return _svgWrap("#D4A520", _pFile);
  },
  "claude.md": function () {
    return _svgWrap("#D97757", _pFile);
  },
  "agents.md": function () {
    return _svgWrap("#D97757", _pFile);
  },
};

function _getFileIcon(name, type, expanded) {
  if (type === "dir") {
    return expanded
      ? _svgWrap("var(--text-muted)", _pFolderOpen)
      : _svgWrap("var(--text-muted)", _pFolder);
  }

  var lower = name.toLowerCase();

  // Check special filenames first
  if (_nameIconMap[lower]) return _nameIconMap[lower]();

  // Dockerfile.* and docker-compose.* patterns
  if (
    lower.indexOf("dockerfile") === 0 ||
    lower.indexOf("docker-compose") === 0
  ) {
    return _svgWrap("#2496ED", _pFile);
  }

  // .gitignore, .gitmodules, .gitattributes
  if (
    lower === ".gitignore" ||
    lower === ".gitmodules" ||
    lower === ".gitattributes"
  ) {
    return _svgWrap("#E44D26", _pFile);
  }

  // README.* variants
  if (lower.indexOf("readme") === 0) {
    return _svgWrap("#6CB6FF", _pFile);
  }

  // Extension-based lookup
  var dot = lower.lastIndexOf(".");
  if (dot >= 0) {
    var ext = lower.slice(dot + 1);
    if (_extIconMap[ext]) return _extIconMap[ext]();
  }

  // Default file icon
  return _svgWrap("var(--text-muted)", _pFile);
}

function _renderNode(node, depth, container) {
  var el = document.createElement("div");
  var classes = "explorer-node";
  if (node.type === "dir") classes += " explorer-node--dir";
  else classes += " explorer-node--file";
  if (node.name.charAt(0) === ".") classes += " explorer-node--hidden";
  el.className = classes;

  el.setAttribute("data-path", node.path);
  el.setAttribute("data-workspace", node.workspace);
  el.setAttribute("tabindex", "0");
  el.setAttribute("role", "treeitem");
  if (node.type === "dir") {
    el.setAttribute("aria-expanded", String(node.expanded));
  }
  el.style.paddingLeft = depth * 16 + 4 + "px";

  // Disclosure triangle
  var toggle = document.createElement("span");
  toggle.className = "explorer-node__toggle";
  if (node.type === "dir") {
    if (node.loading) {
      toggle.textContent = "\u22EF"; // ellipsis
      toggle.className += " explorer-node--loading";
    } else {
      toggle.innerHTML = node.expanded ? _chevDownSvg : _chevRightSvg;
    }
  }
  el.appendChild(toggle);

  // File/folder icon
  var iconSpan = document.createElement("span");
  iconSpan.className = "explorer-node__icon";
  iconSpan.innerHTML = _getFileIcon(node.name, node.type, node.expanded);
  el.appendChild(iconSpan);

  // Name
  var nameSpan = document.createElement("span");
  nameSpan.className = "explorer-node__name";
  nameSpan.textContent = node.name;
  el.appendChild(nameSpan);

  // Click handler
  el.addEventListener("click", function (e) {
    e.stopPropagation();
    if (node.type === "dir") {
      _toggleNode(node);
    } else {
      _openFilePreview(node);
    }
  });

  container.appendChild(el);

  // Render children recursively
  if (node.expanded && node.children) {
    for (var i = 0; i < node.children.length; i++) {
      _renderNode(node.children[i], depth + 1, container);
    }
  }
}

// ---------------------------------------------------------------------------
// Keyboard navigation (Task 4)
// ---------------------------------------------------------------------------

function _initExplorerKeyboard() {
  var treeEl = document.getElementById("explorer-tree");
  if (!treeEl) return;

  treeEl.addEventListener("keydown", function (e) {
    var nodeEls = treeEl.querySelectorAll(".explorer-node");
    if (!nodeEls.length) return;

    var visibleNodes = _getVisibleNodes(_explorerRoots);
    var focused = document.activeElement;
    var idx = -1;
    for (var i = 0; i < nodeEls.length; i++) {
      if (nodeEls[i] === focused) {
        idx = i;
        break;
      }
    }
    if (idx < 0) return;

    var node = visibleNodes[idx];
    if (!node) return;

    switch (e.key) {
      case "ArrowDown":
        e.preventDefault();
        if (idx + 1 < nodeEls.length) nodeEls[idx + 1].focus();
        break;

      case "ArrowUp":
        e.preventDefault();
        if (idx - 1 >= 0) nodeEls[idx - 1].focus();
        break;

      case "ArrowRight":
        e.preventDefault();
        if (node.type === "dir") {
          if (!node.expanded) {
            _expandNode(node);
          } else if (node.children && node.children.length > 0) {
            // Move focus to first child after render
            var nextIdx = idx + 1;
            if (nextIdx < nodeEls.length) nodeEls[nextIdx].focus();
          }
        }
        break;

      case "ArrowLeft":
        e.preventDefault();
        if (node.type === "dir" && node.expanded) {
          _collapseNode(node);
        } else {
          // Move to parent
          var parent = _findParent(_explorerRoots, node);
          if (parent) {
            var parentIdx = visibleNodes.indexOf(parent);
            if (parentIdx >= 0 && parentIdx < nodeEls.length) {
              nodeEls[parentIdx].focus();
            }
          }
        }
        break;

      case "Enter":
        e.preventDefault();
        if (node.type === "dir") {
          _toggleNode(node);
        } else {
          _openFilePreview(node);
        }
        break;
    }
  });
}

// ---------------------------------------------------------------------------
// Reload on workspace change
// ---------------------------------------------------------------------------

function reloadExplorerTree() {
  _explorerLoaded = false;
  _explorerRoots = [];
  var panel = document.getElementById("explorer-panel");
  if (panel && panel.style.display !== "none") {
    // In spec mode, reload the spec tree instead of workspace files.
    if (
      typeof getCurrentMode === "function" &&
      getCurrentMode() === "spec" &&
      typeof loadSpecTree === "function"
    ) {
      loadSpecTree();
    } else {
      _loadExplorerRoots();
    }
  }
}

// ---------------------------------------------------------------------------
// Init (Task 3 + Task 4)
// ---------------------------------------------------------------------------

function _initExplorer() {
  var panel = document.getElementById("explorer-panel");
  if (!panel) return;

  // Restore open/closed state
  var wasOpen = localStorage.getItem(_explorerStorageKeyOpen);
  if (wasOpen === "1") {
    panel.style.display = "";
    var btn = document.getElementById("explorer-toggle-btn");
    if (btn) btn.setAttribute("aria-expanded", "true");
  }

  _initExplorerResize();
  _initExplorerKeyboard();

  // Load tree if panel is already visible.
  if (panel.style.display !== "none") {
    if (
      typeof getCurrentMode === "function" &&
      getCurrentMode() === "spec" &&
      typeof switchExplorerRoot === "function"
    ) {
      switchExplorerRoot("specs");
    } else {
      _loadExplorerRoots();
      _startExplorerRefreshPoll();
    }
  }
}

// Expose globally
window.toggleExplorer = toggleExplorer;
window.reloadExplorerTree = reloadExplorerTree;
window.closeExplorerPreview = closeExplorerPreview;
window._enterEditMode = _enterEditMode;
window._toggleMarkdownView = _toggleMarkdownView;
window._saveFile = _saveFile;
window._discardEdit = _discardEdit;

// Expose internals for testing
window._buildChildNodes = _buildChildNodes;
window._getVisibleNodes = _getVisibleNodes;
window._findParent = _findParent;
window._basename = _basename;
window._classifyFileResponse = _classifyFileResponse;
window._relativePath = _relativePath;
window._isEditDirty = _isEditDirty;
window._getFileIcon = _getFileIcon;
window._expandNode = _expandNode;
window._refreshExpandedNodes = _refreshExpandedNodes;
window._getExplorerRoots = function () {
  return _explorerRoots;
};
window._setExplorerRoots = function (roots) {
  _explorerRoots = roots;
};
window._getTaskPrompts = function () {
  return _taskPrompts;
};
window._setTaskPrompts = function (entries) {
  _taskPrompts = entries;
};
window._renderTaskPromptsSection = _renderTaskPromptsSection;
window._renderTaskPromptEntry = _renderTaskPromptEntry;

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", _initExplorer);
} else {
  _initExplorer();
}
