// --- Spec Dependency Minimap ---
// Renders a small SVG graph showing the focused spec's 1-hop upstream
// (depends_on) and downstream (depended-on-by) neighborhood.

var _minimapStatusColors = {
  complete: "#d4edda",
  validated: "#cce5ff",
  drafted: "#fff3cd",
  vague: "#e2e3e5",
  stale: "#f8d7da",
  archived: "#e2e3e5",
};

// _normalizeDep strips the leading "specs/" prefix from a depends_on path
// so it matches the tree node paths (which are relative to specs/).
function _normalizeDep(dep) {
  if (dep.indexOf("specs/") === 0) return dep.substring(6);
  return dep;
}

// buildReverseDeps builds a reverse dependency index from the spec tree nodes.
// Keys and values use normalized paths (relative to specs/).
function buildReverseDeps(nodes) {
  var reverse = {};
  for (var i = 0; i < nodes.length; i++) {
    var deps = nodes[i].spec && nodes[i].spec.depends_on;
    if (!deps) continue;
    for (var j = 0; j < deps.length; j++) {
      var dep = _normalizeDep(deps[j]);
      if (!reverse[dep]) reverse[dep] = [];
      reverse[dep].push(nodes[i].path);
    }
  }
  return reverse;
}

// renderMinimap draws the 1-hop dependency neighborhood of the focused spec.
function _hideMinimap() {
  var container = document.getElementById("spec-minimap");
  if (container) container.classList.add("hidden");
  var handle = document.getElementById("spec-minimap-resize");
  if (handle) handle.classList.add("hidden");
}

function _showMinimap() {
  var container = document.getElementById("spec-minimap");
  if (container) container.classList.remove("hidden");
  var handle = document.getElementById("spec-minimap-resize");
  if (handle) handle.classList.remove("hidden");
}

function renderMinimap(specPath, treeData) {
  var container = document.getElementById("spec-minimap");
  var svg = document.getElementById("spec-minimap-svg");
  if (!container || !svg) return;

  if (!treeData || !treeData.nodes || !specPath) {
    _hideMinimap();
    return;
  }

  var nodesByPath = {};
  var nodes = treeData.nodes;
  for (var i = 0; i < nodes.length; i++) {
    nodesByPath[nodes[i].path] = nodes[i];
  }

  var focused = nodesByPath[specPath];
  if (!focused) {
    _hideMinimap();
    return;
  }

  var reverseIndex = buildReverseDeps(nodes);

  // Collect 1-hop neighborhood. Normalize depends_on paths to match tree keys.
  // Archived neighbors are hidden unless the user has opted in via the
  // "Show archived" toggle on the explorer.
  var showArchived =
    typeof _showArchived !== "undefined" ? _showArchived : false;
  var includeNeighbor = function (p) {
    var n = nodesByPath[p];
    if (!n || !n.spec) return false;
    if (!showArchived && n.spec.status === "archived") return false;
    return true;
  };
  var upstreamPaths = (focused.spec.depends_on || [])
    .map(_normalizeDep)
    .filter(includeNeighbor);
  var downstreamPaths = (reverseIndex[specPath] || []).filter(includeNeighbor);

  // Hide minimap if no dependencies at all.
  if (upstreamPaths.length === 0 && downstreamPaths.length === 0) {
    _hideMinimap();
    return;
  }

  _showMinimap();

  // Layout parameters.
  var nodeW = 120;
  var nodeH = 28;
  var colGap = 40;
  var rowGap = 8;

  // Three columns: upstream | focused | downstream.
  var upCount = upstreamPaths.length;
  var downCount = downstreamPaths.length;
  var maxRows = Math.max(upCount, 1, downCount);

  var totalW = nodeW * 3 + colGap * 2;
  var totalH = maxRows * (nodeH + rowGap) - rowGap + 16;

  svg.setAttribute("width", totalW);
  svg.setAttribute("height", totalH);
  svg.setAttribute("viewBox", "0 0 " + totalW + " " + totalH);

  var svgNS = "http://www.w3.org/2000/svg";
  svg.innerHTML = "";

  // Set up pan-to-drag on the minimap body.
  _initMinimapPan(container, svg, totalW, totalH);

  // Column x positions.
  var col0x = 0;
  var col1x = nodeW + colGap;
  var col2x = (nodeW + colGap) * 2;

  // Focused node (center column, vertically centered).
  var focusedY = (totalH - nodeH) / 2;
  _drawMinimapNode(svg, svgNS, focused, col1x, focusedY, nodeW, nodeH, true);

  var focusedArchived = focused.spec && focused.spec.status === "archived";

  // Upstream nodes (left column).
  for (var u = 0; u < upCount; u++) {
    var upNode = nodesByPath[upstreamPaths[u]];
    var uy = _columnY(u, upCount, totalH, nodeH, rowGap);
    _drawMinimapNode(svg, svgNS, upNode, col0x, uy, nodeW, nodeH, false);
    var upArchived =
      focusedArchived ||
      (upNode && upNode.spec && upNode.spec.status === "archived");
    _drawMinimapEdge(
      svg,
      svgNS,
      col0x + nodeW,
      uy + nodeH / 2,
      col1x,
      focusedY + nodeH / 2,
      upArchived,
    );
  }

  // Downstream nodes (right column).
  for (var d = 0; d < downCount; d++) {
    var downNode = nodesByPath[downstreamPaths[d]];
    var dy = _columnY(d, downCount, totalH, nodeH, rowGap);
    _drawMinimapNode(svg, svgNS, downNode, col2x, dy, nodeW, nodeH, false);
    var downArchived =
      focusedArchived ||
      (downNode && downNode.spec && downNode.spec.status === "archived");
    _drawMinimapEdge(
      svg,
      svgNS,
      col1x + nodeW,
      focusedY + nodeH / 2,
      col2x,
      dy + nodeH / 2,
      downArchived,
    );
  }
}

// _columnY vertically centers a set of nodes within the total height.
function _columnY(index, count, totalH, nodeH, rowGap) {
  var colH = count * (nodeH + rowGap) - rowGap;
  var startY = (totalH - colH) / 2;
  return startY + index * (nodeH + rowGap);
}

// _drawMinimapNode renders a colored rectangle with a label.
function _drawMinimapNode(svg, ns, node, x, y, w, h, isFocused) {
  var status = node.spec ? node.spec.status : "";
  var fill = _minimapStatusColors[status] || "#e2e3e5";
  var title = node.spec ? node.spec.title || node.path : node.path;

  var rect = document.createElementNS(ns, "rect");
  rect.setAttribute("x", x);
  rect.setAttribute("y", y);
  rect.setAttribute("width", w);
  rect.setAttribute("height", h);
  rect.setAttribute("rx", 4);
  rect.setAttribute("fill", fill);
  rect.setAttribute("stroke", isFocused ? "#0366d6" : "#ccc");
  rect.setAttribute("stroke-width", isFocused ? 2 : 1);
  if (status === "archived") {
    rect.setAttribute("class", "spec-minimap__node--archived");
    rect.setAttribute("stroke-dasharray", "4 3");
  }
  rect.style.cursor = "pointer";
  rect.setAttribute("data-spec-path", node.path);
  rect.addEventListener("click", function () {
    if (
      typeof focusSpec === "function" &&
      typeof activeWorkspaces !== "undefined"
    ) {
      focusSpec(node.path, activeWorkspaces[0] || "");
    }
  });
  svg.appendChild(rect);

  // Truncate title to fit.
  var maxChars = Math.floor(w / 7);
  var label =
    title.length > maxChars
      ? title.substring(0, maxChars - 1) + "\u2026"
      : title;

  var text = document.createElementNS(ns, "text");
  text.setAttribute("x", x + w / 2);
  text.setAttribute("y", y + h / 2 + 4);
  text.setAttribute("text-anchor", "middle");
  text.setAttribute("font-size", "10");
  text.setAttribute("fill", "#333");
  text.setAttribute("pointer-events", "none");
  text.textContent = label;
  svg.appendChild(text);
}

// _drawMinimapEdge renders a line between two points. When either endpoint is
// an archived spec, the edge is rendered dashed to signal it is out of the
// live dependency graph.
function _drawMinimapEdge(svg, ns, x1, y1, x2, y2, archived) {
  var line = document.createElementNS(ns, "line");
  line.setAttribute("x1", x1);
  line.setAttribute("y1", y1);
  line.setAttribute("x2", x2);
  line.setAttribute("y2", y2);
  line.setAttribute("stroke", "#999");
  line.setAttribute("stroke-width", 1);
  if (archived) {
    line.setAttribute("class", "spec-minimap__edge--archived");
    line.setAttribute("stroke-dasharray", "4 3");
  }
  svg.appendChild(line);
}

// --- Minimap pan (drag to scroll) ---

var _minimapPanState = null;

function _initMinimapPan(container, svg, contentW, contentH) {
  var body = container.querySelector(".spec-minimap__body");
  if (!body) return;

  // Remove previous handlers if any.
  if (body._panMouseDown) {
    body.removeEventListener("mousedown", body._panMouseDown);
  }

  var panX = 0;
  var panY = 0;

  body._panMouseDown = function (e) {
    // Don't pan if clicking on a node (let the click handler fire).
    if (e.target.tagName === "rect" || e.target.tagName === "text") return;

    e.preventDefault();
    var startX = e.clientX;
    var startY = e.clientY;
    var startPanX = panX;
    var startPanY = panY;

    function onMouseMove(ev) {
      panX = startPanX - (ev.clientX - startX);
      panY = startPanY - (ev.clientY - startY);
      // Clamp panning so the graph doesn't go fully off-screen.
      var bodyRect = body.getBoundingClientRect
        ? body.getBoundingClientRect()
        : { width: contentW, height: contentH };
      panX = Math.max(0, Math.min(panX, contentW - bodyRect.width));
      panY = Math.max(0, Math.min(panY, contentH - bodyRect.height));
      svg.setAttribute(
        "viewBox",
        panX + " " + panY + " " + bodyRect.width + " " + bodyRect.height,
      );
    }

    function onMouseUp() {
      document.removeEventListener("mousemove", onMouseMove);
      document.removeEventListener("mouseup", onMouseUp);
    }

    document.addEventListener("mousemove", onMouseMove);
    document.addEventListener("mouseup", onMouseUp);
  };

  body.addEventListener("mousedown", body._panMouseDown);

  // Initial viewBox: fit to container width, show from origin.
  var bodyRect = body.getBoundingClientRect
    ? body.getBoundingClientRect()
    : { width: contentW, height: contentH };
  svg.setAttribute("width", "100%");
  svg.removeAttribute("height");
  svg.setAttribute(
    "viewBox",
    "0 0 " +
      Math.max(contentW, bodyRect.width) +
      " " +
      Math.max(contentH, bodyRect.height),
  );
}

// --- Minimap resize ---
var _minimapStorageKey = "wallfacer-minimap-height";

function _initMinimapResize() {
  var handle = document.getElementById("spec-minimap-resize");
  var panel = document.getElementById("spec-minimap");
  if (!handle || !panel) return;

  var stored = localStorage.getItem(_minimapStorageKey);
  if (stored) {
    var h = parseInt(stored, 10);
    if (h >= 80 && h <= 400) panel.style.height = h + "px";
  }

  handle.addEventListener("mousedown", function (e) {
    e.preventDefault();
    var startY = e.clientY;
    var startH = panel.offsetHeight;
    document.body.style.userSelect = "none";
    document.body.style.cursor = "row-resize";

    function onMouseMove(ev) {
      // Resize handle is above the minimap, so dragging up = smaller.
      var delta = ev.clientY - startY;
      var newH = Math.min(400, Math.max(80, startH - delta));
      panel.style.height = newH + "px";
    }

    function onMouseUp() {
      document.removeEventListener("mousemove", onMouseMove);
      document.removeEventListener("mouseup", onMouseUp);
      document.body.style.userSelect = "";
      document.body.style.cursor = "";
      localStorage.setItem(
        _minimapStorageKey,
        parseInt(panel.style.height, 10),
      );
    }

    document.addEventListener("mousemove", onMouseMove);
    document.addEventListener("mouseup", onMouseUp);
  });
}

document.addEventListener("DOMContentLoaded", function () {
  _initMinimapResize();
});
