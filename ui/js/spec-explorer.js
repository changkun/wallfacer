// --- Spec Explorer ---
// Renders the spec tree from the GET /api/specs/tree API in the explorer
// panel when in spec mode. Shows status badges, recursive progress counts,
// and collapsible subtrees.

var _specTreeData = null;
var _specTreeTimer = null;
var _specExpandedPaths = new Set(
  JSON.parse(localStorage.getItem("wallfacer-spec-expanded") || "[]"),
);
var _explorerRootMode = "workspace"; // "workspace" | "specs"
var _specStatusFilter = localStorage.getItem("wallfacer-spec-filter") || "all";

// Status → icon mapping.
var _specStatusIcons = {
  complete: "\u2705",
  validated: "\u2714",
  drafted: "\uD83D\uDCDD",
  vague: "\uD83D\uDCAD",
  stale: "\u26A0\uFE0F",
};

function loadSpecTree() {
  fetch(Routes.specs.tree(), { headers: withBearerHeaders() })
    .then(function (r) {
      return r.json();
    })
    .then(function (data) {
      _specTreeData = data;
      renderSpecTree();
    })
    .catch(function (err) {
      console.error("spec tree load error:", err);
    });
}

// switchExplorerRoot switches the explorer panel between workspace files
// and spec tree views. Called by switchMode() in spec-mode.js.
function switchExplorerRoot(mode) {
  if (mode === _explorerRootMode) return;
  _explorerRootMode = mode;

  var treeEl = document.getElementById("explorer-tree");
  if (treeEl) treeEl.innerHTML = "";

  _stopSpecTreePoll();

  // Show/hide the "Show workspace files" toggle and status filter.
  var toggle = document.getElementById("spec-explorer-workspace-toggle");
  if (toggle) {
    toggle.classList.toggle("hidden", mode !== "specs");
  }
  var filterEl = document.getElementById("spec-status-filter");
  if (filterEl) {
    filterEl.classList.toggle("hidden", mode !== "specs");
    if (mode === "specs") {
      filterEl.value = _specStatusFilter;
    }
  }

  if (mode === "specs") {
    loadSpecTree();
    _startSpecTreePoll();
  } else {
    // Restore workspace file explorer.
    if (typeof _loadExplorerRoots === "function") {
      _loadExplorerRoots();
    }
    if (typeof _startExplorerRefreshPoll === "function") {
      _startExplorerRefreshPoll();
    }
  }
}

// toggleSpecWorkspaceFiles shows or hides workspace files below the spec tree.
function toggleSpecWorkspaceFiles(checked) {
  var treeEl = document.getElementById("explorer-tree");
  if (!treeEl) return;

  // Remove any existing workspace section.
  var existing = document.getElementById("spec-workspace-files");
  if (existing) existing.remove();

  if (checked) {
    // Add a workspace files section below the spec tree.
    var section = document.createElement("div");
    section.id = "spec-workspace-files";
    section.className = "spec-workspace-files";
    section.innerHTML =
      '<div class="spec-workspace-files__header">Workspace Files</div>';
    treeEl.parentNode.insertBefore(section, treeEl.nextSibling);
  }
}

function _startSpecTreePoll() {
  _stopSpecTreePoll();
  _specTreeTimer = setInterval(function () {
    loadSpecTree();
  }, 3000);
}

function _stopSpecTreePoll() {
  if (_specTreeTimer) {
    clearInterval(_specTreeTimer);
    _specTreeTimer = null;
  }
}

// filterSpecTree sets the status filter and re-renders.
function filterSpecTree(filter) {
  _specStatusFilter = filter;
  localStorage.setItem("wallfacer-spec-filter", filter);
  renderSpecTree();
}

// _nodeMatchesFilter checks if a node or any of its descendants match
// the current status filter. Non-leaf nodes are visible if any descendant matches.
function _nodeMatchesFilter(node, nodesByPath) {
  if (_specStatusFilter === "all") return true;

  var spec = node.spec;
  if (!spec) return false;

  var status = spec.status;
  var match = false;

  if (_specStatusFilter === "incomplete") {
    match = status !== "complete";
  } else {
    match = status === _specStatusFilter;
  }

  // Leaf nodes: match directly.
  if (node.is_leaf) return match;

  // Non-leaf: visible if self matches or any descendant matches.
  if (match) return true;
  var children = node.children || [];
  for (var i = 0; i < children.length; i++) {
    var child = nodesByPath[children[i]];
    if (child && _nodeMatchesFilter(child, nodesByPath)) return true;
  }
  return false;
}

// renderSpecTree renders the spec tree into the explorer-tree container.
function renderSpecTree() {
  var treeEl = document.getElementById("explorer-tree");
  if (!treeEl || !_specTreeData) return;

  // Build a tree structure from the flat nodes array.
  var nodesByPath = {};
  var roots = [];
  var nodes = _specTreeData.nodes || [];

  for (var i = 0; i < nodes.length; i++) {
    nodesByPath[nodes[i].path] = nodes[i];
  }

  for (var j = 0; j < nodes.length; j++) {
    if (nodes[j].depth === 0) {
      roots.push(nodes[j]);
    }
  }

  var html = "";
  for (var k = 0; k < roots.length; k++) {
    html += _renderSpecNode(roots[k], nodesByPath);
  }

  treeEl.innerHTML = html;

  // Attach click handlers.
  var nodeEls = treeEl.querySelectorAll("[data-spec-path]");
  for (var n = 0; n < nodeEls.length; n++) {
    nodeEls[n].addEventListener("click", _onSpecNodeClick);
  }

  // Attach toggle handlers.
  var toggleEls = treeEl.querySelectorAll(".spec-node-toggle");
  for (var t = 0; t < toggleEls.length; t++) {
    toggleEls[t].addEventListener("click", _onSpecToggleClick);
  }
}

function _renderSpecNode(node, nodesByPath) {
  if (!_nodeMatchesFilter(node, nodesByPath)) return "";

  var spec = node.spec;
  if (!spec) return "";

  var icon = _specStatusIcons[spec.status] || "";
  var title = spec.title || node.path;
  var isExpanded = _specExpandedPaths.has(node.path);
  var hasChildren = node.children && node.children.length > 0;

  var progress = "";
  if (
    !node.is_leaf &&
    _specTreeData.progress &&
    _specTreeData.progress[node.path]
  ) {
    var p = _specTreeData.progress[node.path];
    progress =
      ' <span class="spec-node-progress">' +
      p.Complete +
      "/" +
      p.Total +
      "</span>";
  }

  var indent = node.depth * 16;
  var classes = "spec-node";
  if (node.is_leaf) classes += " spec-node--leaf";
  if (
    typeof getFocusedSpecPath === "function" &&
    getFocusedSpecPath() === node.path
  ) {
    classes += " spec-node--focused";
  }

  var html =
    '<div class="' +
    classes +
    '" data-spec-path="' +
    escapeHtml(node.path) +
    '" style="padding-left: ' +
    indent +
    'px">';

  if (hasChildren) {
    html +=
      '<span class="spec-node-toggle" data-path="' +
      escapeHtml(node.path) +
      '">' +
      (isExpanded ? "\u25BE" : "\u25B8") +
      "</span> ";
  } else {
    html += '<span class="spec-node-toggle-placeholder"></span> ';
  }

  html +=
    '<span class="spec-node-icon">' +
    icon +
    "</span> " +
    '<span class="spec-node-title">' +
    escapeHtml(title) +
    "</span>" +
    progress +
    "</div>";

  // Render children if expanded.
  if (hasChildren && isExpanded) {
    for (var i = 0; i < node.children.length; i++) {
      var child = nodesByPath[node.children[i]];
      if (child) {
        html += _renderSpecNode(child, nodesByPath);
      }
    }
  }

  return html;
}

function _onSpecNodeClick(e) {
  // Don't handle if the toggle arrow was clicked.
  if (e.target.classList && e.target.classList.contains("spec-node-toggle")) {
    return;
  }
  var el = e.currentTarget;
  var path = el.getAttribute("data-spec-path");
  if (!path) return;

  // Determine workspace from activeWorkspaces.
  var ws =
    typeof activeWorkspaces !== "undefined" && activeWorkspaces.length > 0
      ? activeWorkspaces[0]
      : "";

  if (typeof focusSpec === "function") {
    focusSpec(path, ws);
    renderSpecTree(); // re-render to update focused highlight
  }
}

function _onSpecToggleClick(e) {
  e.stopPropagation();
  var path = e.currentTarget.getAttribute("data-path");
  if (!path) return;

  if (_specExpandedPaths.has(path)) {
    _specExpandedPaths.delete(path);
  } else {
    _specExpandedPaths.add(path);
  }

  localStorage.setItem(
    "wallfacer-spec-expanded",
    JSON.stringify(Array.from(_specExpandedPaths)),
  );
  renderSpecTree();
}
