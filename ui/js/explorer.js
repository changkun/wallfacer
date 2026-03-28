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
      node.children = _buildChildNodes(entries || [], node.path, node.workspace);
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

// Stub for Task 5 — file preview modal.
function _openFilePreview(_node) {
  // Will be implemented in Task 5.
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

// Expose internals for testing
window._buildChildNodes = _buildChildNodes;
window._getVisibleNodes = _getVisibleNodes;
window._findParent = _findParent;
window._basename = _basename;

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", _initExplorer);
} else {
  _initExplorer();
}
