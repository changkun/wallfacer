// --- Spec Dependency Minimap ---
// Renders a small SVG graph showing the focused spec's 1-hop upstream
// (depends_on) and downstream (depended-on-by) neighborhood.

var _minimapStatusColors = {
  complete: "#d4edda",
  validated: "#cce5ff",
  drafted: "#fff3cd",
  vague: "#e2e3e5",
  stale: "#f8d7da",
};

// buildReverseDeps builds a reverse dependency index from the spec tree nodes.
function buildReverseDeps(nodes) {
  var reverse = {};
  for (var i = 0; i < nodes.length; i++) {
    var deps = nodes[i].spec && nodes[i].spec.depends_on;
    if (!deps) continue;
    for (var j = 0; j < deps.length; j++) {
      if (!reverse[deps[j]]) reverse[deps[j]] = [];
      reverse[deps[j]].push(nodes[i].path);
    }
  }
  return reverse;
}

// renderMinimap draws the 1-hop dependency neighborhood of the focused spec.
function renderMinimap(specPath, treeData) {
  var container = document.getElementById("spec-minimap");
  var svg = document.getElementById("spec-minimap-svg");
  if (!container || !svg) return;

  if (!treeData || !treeData.nodes || !specPath) {
    container.classList.add("hidden");
    return;
  }

  var nodesByPath = {};
  var nodes = treeData.nodes;
  for (var i = 0; i < nodes.length; i++) {
    nodesByPath[nodes[i].path] = nodes[i];
  }

  var focused = nodesByPath[specPath];
  if (!focused) {
    container.classList.add("hidden");
    return;
  }

  var reverseIndex = buildReverseDeps(nodes);

  // Collect 1-hop neighborhood.
  var upstreamPaths = (focused.spec.depends_on || []).filter(function (p) {
    return !!nodesByPath[p];
  });
  var downstreamPaths = (reverseIndex[specPath] || []).filter(function (p) {
    return !!nodesByPath[p];
  });

  // Hide minimap if no dependencies at all.
  if (upstreamPaths.length === 0 && downstreamPaths.length === 0) {
    container.classList.add("hidden");
    return;
  }

  container.classList.remove("hidden");

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

  // Column x positions.
  var col0x = 0;
  var col1x = nodeW + colGap;
  var col2x = (nodeW + colGap) * 2;

  // Focused node (center column, vertically centered).
  var focusedY = (totalH - nodeH) / 2;
  _drawMinimapNode(svg, svgNS, focused, col1x, focusedY, nodeW, nodeH, true);

  // Upstream nodes (left column).
  for (var u = 0; u < upCount; u++) {
    var upNode = nodesByPath[upstreamPaths[u]];
    var uy = _columnY(u, upCount, totalH, nodeH, rowGap);
    _drawMinimapNode(svg, svgNS, upNode, col0x, uy, nodeW, nodeH, false);
    _drawMinimapEdge(
      svg,
      svgNS,
      col0x + nodeW,
      uy + nodeH / 2,
      col1x,
      focusedY + nodeH / 2,
    );
  }

  // Downstream nodes (right column).
  for (var d = 0; d < downCount; d++) {
    var downNode = nodesByPath[downstreamPaths[d]];
    var dy = _columnY(d, downCount, totalH, nodeH, rowGap);
    _drawMinimapNode(svg, svgNS, downNode, col2x, dy, nodeW, nodeH, false);
    _drawMinimapEdge(
      svg,
      svgNS,
      col1x + nodeW,
      focusedY + nodeH / 2,
      col2x,
      dy + nodeH / 2,
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

// _drawMinimapEdge renders a line between two points.
function _drawMinimapEdge(svg, ns, x1, y1, x2, y2) {
  var line = document.createElementNS(ns, "line");
  line.setAttribute("x1", x1);
  line.setAttribute("y1", y1);
  line.setAttribute("x2", x2);
  line.setAttribute("y2", y2);
  line.setAttribute("stroke", "#999");
  line.setAttribute("stroke-width", 1);
  svg.appendChild(line);
}
