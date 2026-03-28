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

  // Load tree on first open
  if (isHidden && !_explorerLoaded) {
    _loadExplorerRoots();
  }
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
  _renderTree();
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
      node.children = _buildChildNodes(
        entries || [],
        node.path,
        node.workspace,
      );
      node.expanded = true;
      node.loading = false;
      _renderTree();
    })
    .catch(function () {
      node.loading = false;
      _renderTree();
    });
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

      contentEl.innerHTML = _renderHighlightedContent(
        result.content,
        node.name,
      );
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

function _discardEdit() {
  if (_isEditDirty()) {
    if (!confirm("You have unsaved changes. Discard?")) return;
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

function closeExplorerPreview() {
  if (_isEditDirty()) {
    if (!confirm("You have unsaved changes. Discard?")) return;
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

  for (var i = 0; i < _explorerRoots.length; i++) {
    _renderNode(_explorerRoots[i], 0, container);
  }
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
      toggle.textContent = node.expanded ? "\u25BC" : "\u25B6";
    }
  }
  el.appendChild(toggle);

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
    _loadExplorerRoots();
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

  // Load tree if panel is already visible
  if (panel.style.display !== "none") {
    _loadExplorerRoots();
  }
}

// Expose globally
window.toggleExplorer = toggleExplorer;
window.reloadExplorerTree = reloadExplorerTree;
window.closeExplorerPreview = closeExplorerPreview;
window._enterEditMode = _enterEditMode;
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

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", _initExplorer);
} else {
  _initExplorer();
}
