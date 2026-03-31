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
var _selectedSpecPaths = new Set();
var _lastCheckedSpecIndex = -1;

// Status → icon mapping.
var _specStatusIcons = {
  complete: "\u2705",
  validated: "\u2714",
  drafted: "\uD83D\uDCDD",
  vague: "\uD83D\uDCAD",
  stale: "\u26A0\uFE0F",
};

function loadSpecTree() {
  // Show loading indicator while fetching.
  var treeEl = document.getElementById("explorer-tree");
  if (treeEl && !_specTreeData) {
    treeEl.innerHTML = '<div class="spec-loading">Loading specs\u2026</div>';
  }

  fetch(Routes.specs.tree(), { headers: withBearerHeaders() })
    .then(function (r) {
      return r.json();
    })
    .then(function (data) {
      _specTreeData = data;
      renderSpecTree();
      // Update minimap with fresh tree data if a spec is focused.
      if (
        typeof renderMinimap === "function" &&
        typeof getFocusedSpecPath === "function" &&
        getFocusedSpecPath()
      ) {
        renderMinimap(getFocusedSpecPath(), _specTreeData);
      }
    })
    .catch(function (err) {
      console.error("spec tree load error:", err);
      if (treeEl) {
        treeEl.innerHTML =
          '<div class="spec-loading">Failed to load specs</div>';
      }
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
  var dispatchBar = document.getElementById("spec-dispatch-bar");
  if (dispatchBar) {
    dispatchBar.classList.toggle("hidden", mode !== "specs");
  }
  var minimap = document.getElementById("spec-minimap");
  if (minimap && mode !== "specs") {
    minimap.classList.add("hidden");
  }

  if (mode === "specs") {
    // Stop the workspace explorer's refresh poll so it doesn't overwrite
    // the spec tree with workspace files.
    if (typeof _stopExplorerRefreshPoll === "function") {
      _stopExplorerRefreshPoll();
    }
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
      '<div class="spec-workspace-files__header">Workspace Files</div>' +
      '<div class="spec-loading">Loading files\u2026</div>';
    treeEl.parentNode.insertBefore(section, treeEl.nextSibling);

    // Load workspace root entries via the explorer tree API.
    var workspaces =
      typeof activeWorkspaces !== "undefined" && Array.isArray(activeWorkspaces)
        ? activeWorkspaces
        : [];
    var html =
      '<div class="spec-workspace-files__header">Workspace Files</div>';
    var pending = workspaces.length;
    if (pending === 0) {
      section.innerHTML = html;
      return;
    }
    for (var i = 0; i < workspaces.length; i++) {
      (function (ws) {
        var url =
          Routes.explorer.tree() +
          "?path=" +
          encodeURIComponent(ws) +
          "&workspace=" +
          encodeURIComponent(ws);
        fetch(url, { headers: withBearerHeaders() })
          .then(function (r) {
            return r.json();
          })
          .then(function (entries) {
            for (var j = 0; j < entries.length; j++) {
              var e = entries[j];
              var icon = e.type === "dir" ? "\uD83D\uDCC1" : "\uD83D\uDCC4";
              html +=
                '<div class="spec-node" style="padding-left: 8px">' +
                '<span class="spec-node-icon">' +
                icon +
                "</span> " +
                '<span class="spec-node-title">' +
                escapeHtml(e.name) +
                "</span></div>";
            }
          })
          .catch(function () {})
          .finally(function () {
            pending--;
            if (pending <= 0) {
              var el = document.getElementById("spec-workspace-files");
              if (el) el.innerHTML = html;
            }
          });
      })(workspaces[i]);
    }
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

  // Build a lookup and group root nodes by track.
  var nodesByPath = {};
  var trackGroups = {}; // track name -> [nodes]
  var trackOrder = [];
  var nodes = _specTreeData.nodes || [];

  for (var i = 0; i < nodes.length; i++) {
    nodesByPath[nodes[i].path] = nodes[i];
  }

  for (var j = 0; j < nodes.length; j++) {
    if (nodes[j].depth === 0) {
      var track = (nodes[j].spec && nodes[j].spec.track) || "other";
      if (!trackGroups[track]) {
        trackGroups[track] = [];
        trackOrder.push(track);
      }
      trackGroups[track].push(nodes[j]);
    }
  }

  var html = "";
  for (var ti = 0; ti < trackOrder.length; ti++) {
    var track = trackOrder[ti];
    var trackExpanded = _specExpandedPaths.has("__track__" + track);
    html +=
      '<div class="spec-track-header" data-track="' +
      escapeHtml(track) +
      '">' +
      '<span class="spec-node-toggle" data-path="__track__' +
      escapeHtml(track) +
      '">' +
      (trackExpanded ? "\u25BE" : "\u25B8") +
      "</span> " +
      '<span class="spec-track-name">' +
      escapeHtml(track) +
      "</span></div>";
    if (trackExpanded) {
      var trackRoots = trackGroups[track];
      for (var k = 0; k < trackRoots.length; k++) {
        html += _renderSpecNode(trackRoots[k], nodesByPath);
      }
    }
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

  // Track headers: clicking the header text also toggles.
  var trackHeaders = treeEl.querySelectorAll(".spec-track-header");
  for (var th = 0; th < trackHeaders.length; th++) {
    trackHeaders[th].addEventListener("click", function (e) {
      var track = e.currentTarget.getAttribute("data-track");
      if (!track) return;
      var key = "__track__" + track;
      if (_specExpandedPaths.has(key)) {
        _specExpandedPaths.delete(key);
      } else {
        _specExpandedPaths.add(key);
      }
      localStorage.setItem(
        "wallfacer-spec-expanded",
        JSON.stringify(Array.from(_specExpandedPaths)),
      );
      renderSpecTree();
    });
  }

  // Attach checkbox handlers.
  var checkboxEls = treeEl.querySelectorAll(".spec-select-checkbox");
  for (var c = 0; c < checkboxEls.length; c++) {
    checkboxEls[c].addEventListener("change", _onSpecCheckboxChange);
    checkboxEls[c].addEventListener("click", function (e) {
      e.stopPropagation();
    });
  }

  _updateDispatchSelectedButton();
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

  // Checkbox for validated leaf specs (multi-select dispatch).
  if (node.is_leaf && spec.status === "validated") {
    var checked = _selectedSpecPaths.has(node.path) ? " checked" : "";
    html +=
      '<input type="checkbox" class="spec-select-checkbox" data-spec-select="' +
      escapeHtml(node.path) +
      '"' +
      checked +
      "> ";
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
  // Don't handle if the toggle arrow or checkbox was clicked.
  if (e.target.classList && e.target.classList.contains("spec-node-toggle")) {
    return;
  }
  if (
    e.target.classList &&
    e.target.classList.contains("spec-select-checkbox")
  ) {
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

// --- Multi-select dispatch ---

function _onSpecCheckboxChange(e) {
  var path = e.target.getAttribute("data-spec-select");
  if (!path) return;

  // Shift-click range selection (shiftKey is available on change events
  // in most browsers; fall back to click listener if not).
  if (e.shiftKey) {
    var checkboxes = document.querySelectorAll
      ? Array.from(document.querySelectorAll(".spec-select-checkbox"))
      : [];
    var currentIndex = checkboxes.indexOf(e.target);
    if (_lastCheckedSpecIndex >= 0 && currentIndex >= 0) {
      var start = Math.min(_lastCheckedSpecIndex, currentIndex);
      var end = Math.max(_lastCheckedSpecIndex, currentIndex);
      for (var i = start; i <= end; i++) {
        var cbPath = checkboxes[i].getAttribute("data-spec-select");
        if (cbPath) {
          if (e.target.checked) {
            _selectedSpecPaths.add(cbPath);
          } else {
            _selectedSpecPaths.delete(cbPath);
          }
          checkboxes[i].checked = e.target.checked;
        }
      }
    }
  }

  if (e.target.checked) {
    _selectedSpecPaths.add(path);
  } else {
    _selectedSpecPaths.delete(path);
  }

  // Track last checked index for shift-click.
  var allCheckboxes = document.querySelectorAll
    ? Array.from(document.querySelectorAll(".spec-select-checkbox"))
    : [];
  _lastCheckedSpecIndex = allCheckboxes.indexOf(e.target);

  _updateDispatchSelectedButton();
}

function _updateDispatchSelectedButton() {
  var btn = document.getElementById("spec-dispatch-selected-btn");
  if (!btn) return;
  var count = _selectedSpecPaths.size;
  if (count > 0) {
    btn.classList.remove("hidden");
    btn.textContent = "Dispatch Selected (" + count + ")";
  } else {
    btn.classList.add("hidden");
  }
}

// dispatchSelectedSpecs is a stub — actual dispatch logic is wired by
// the dispatch-workflow spec.
function dispatchSelectedSpecs() {
  var paths = Array.from(_selectedSpecPaths);
  console.log("dispatch selected specs:", paths);
}
