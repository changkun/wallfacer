// unified-graph.js — Merge spec tree + task DAG into a single node/edge
// set for the Dep Graph tab.
//
// Semantic axes:
//   * Specs form a containment hierarchy (parent → child via the file tree)
//     plus design-level cross-dependencies (spec.depends_on[]).
//   * Tasks form a runtime DAG (task.depends_on[]).
//   * A leaf spec "materializes" into a task via spec.dispatched_task_id,
//     linking the two graphs.
//
// Node and edge schema:
//   node: { id, kind, label, status, extra }
//     kind: "spec" | "task"
//     extra for spec: { path, isLeaf, depth, effort, track }
//     extra for task: { dispatched: bool, archived: bool }
//   edge: { from, to, kind }
//     kind: "containment" | "spec_dep" | "dispatch" | "task_dep"
//
// Edge direction convention: `from` comes before `to` in the visual flow
// (left-to-right layered layout). Concretely:
//   * containment: parent → child (parent is the container)
//   * dispatch:    spec  → task  (spec materializes into task)
//   * spec_dep:    prereq-spec → dependent-spec (B must exist before A, A.depends_on=[B] → B→A)
//   * task_dep:    prereq-task → dependent-task (same shape as spec_dep)
// This matches the existing depgraph.js convention so a single topological
// layout places all edges consistently.
//
// Filtering rules applied here:
//   * Specs with status: "archived" are skipped entirely (nodes and edges)
//     to match the explorer's default behaviour.
//   * Archived tasks are skipped unless explicitly included via opts.
//   * Specs whose path (or any ancestor's path) is in `opts.collapsedSpecs`
//     are treated as hidden for rendering. Their dispatched tasks and
//     their outgoing/incoming edges drop out of the graph. The collapsed
//     spec itself stays visible with a hasChildren=true flag so the
//     renderer can draw a toggle handle.

(function () {
  "use strict";

  // buildUnifiedGraph returns {nodes, edges} given the board's task list
  // and the /api/specs/tree response's `nodes` array (NodeResponse[]).
  //
  // `specNodes` may be empty (workspace with no specs) or undefined; in that
  // case the result is just the task DAG.
  //
  // Options:
  //   includeArchivedSpecs (default false)
  //   includeArchivedTasks (default false) — archived tasks filtered out
  //   collapsedSpecs      (Set<string>|null) — spec paths whose descendants
  //                        should be hidden. Each collapsed spec stays in
  //                        the graph as a handle; its children + edges
  //                        are filtered out.
  function buildUnifiedGraph(tasks, specNodes, opts) {
    opts = opts || {};
    var includeArchivedSpecs = !!opts.includeArchivedSpecs;
    var includeArchivedTasks = !!opts.includeArchivedTasks;
    var collapsedSpecs =
      opts.collapsedSpecs && typeof opts.collapsedSpecs.has === "function"
        ? opts.collapsedSpecs
        : null;
    tasks = Array.isArray(tasks) ? tasks : [];
    specNodes = Array.isArray(specNodes) ? specNodes : [];

    var nodes = [];
    var edges = [];

    // Index specs by path so dependency edges can look up whether a target
    // is present (skipped specs drop their in/out edges).
    var specByPath = Object.create(null);
    for (var i = 0; i < specNodes.length; i++) {
      var n = specNodes[i];
      if (!n || !n.spec) continue;
      if (!includeArchivedSpecs && n.spec.status === "archived") continue;
      specByPath[n.path] = n;
    }

    // Compute the set of spec paths hidden by a collapsed ancestor. We
    // perform a BFS from each collapsed spec through its children.
    var hiddenSpecs = Object.create(null);
    if (collapsedSpecs) {
      var queue = [];
      collapsedSpecs.forEach(function (p) {
        // Collapsed spec itself is NOT hidden — only its descendants.
        var root = specByPath[p];
        if (!root) return;
        var rootChildren = Array.isArray(root.children) ? root.children : [];
        for (var qi = 0; qi < rootChildren.length; qi++) {
          queue.push(rootChildren[qi]);
        }
      });
      while (queue.length > 0) {
        var cur = queue.shift();
        if (hiddenSpecs[cur]) continue;
        hiddenSpecs[cur] = true;
        var curNode = specByPath[cur];
        if (!curNode) continue;
        var curChildren = Array.isArray(curNode.children)
          ? curNode.children
          : [];
        for (var qj = 0; qj < curChildren.length; qj++) {
          queue.push(curChildren[qj]);
        }
      }
    }

    // Spec nodes + containment edges (parent spec → child spec).
    // We draw containment using the NodeResponse.children array rather than
    // inferring from depth so the edge set mirrors the authoritative tree.
    for (var path in specByPath) {
      if (hiddenSpecs[path]) continue;
      var node = specByPath[path];
      var spec = node.spec;
      var childrenAll = Array.isArray(node.children) ? node.children : [];
      var hasVisibleChildren = false;
      for (var ci = 0; ci < childrenAll.length; ci++) {
        if (specByPath[childrenAll[ci]]) {
          hasVisibleChildren = true;
          break;
        }
      }
      var isCollapsed = !!(
        collapsedSpecs &&
        collapsedSpecs.has(path) &&
        hasVisibleChildren
      );
      nodes.push({
        id: "spec:" + path,
        kind: "spec",
        label: spec.title || _basename(path),
        status: spec.status || "",
        extra: {
          path: path,
          isLeaf: !!node.is_leaf,
          depth: typeof node.depth === "number" ? node.depth : 0,
          effort: spec.effort || "",
          track: spec.track || "",
          // hasChildren reflects the *tree*, not the filtered graph. Even
          // a collapsed spec has hasChildren=true so the renderer draws
          // its toggle.
          hasChildren: hasVisibleChildren,
          collapsed: isCollapsed,
        },
      });
      // Skip containment edges out of a collapsed spec — children are
      // hidden so the edges would dangle.
      if (collapsedSpecs && collapsedSpecs.has(path)) continue;
      for (var c = 0; c < childrenAll.length; c++) {
        var childPath = childrenAll[c];
        if (!specByPath[childPath]) continue;
        if (hiddenSpecs[childPath]) continue;
        edges.push({
          from: "spec:" + path,
          to: "spec:" + childPath,
          kind: "containment",
        });
      }
    }

    // Spec → spec design-level dependencies (depends_on).
    // A.depends_on = [B] means B must come before A: emit edge B → A.
    for (var depPath in specByPath) {
      if (hiddenSpecs[depPath]) continue;
      var depSpec = specByPath[depPath].spec;
      var dependsOn = Array.isArray(depSpec.depends_on) ? depSpec.depends_on : [];
      for (var d = 0; d < dependsOn.length; d++) {
        var target = dependsOn[d];
        if (!specByPath[target]) continue;
        if (hiddenSpecs[target]) continue;
        edges.push({
          from: "spec:" + target,
          to: "spec:" + depPath,
          kind: "spec_dep",
        });
      }
    }

    // Task nodes + task → task runtime deps.
    var taskById = Object.create(null);
    for (var t = 0; t < tasks.length; t++) {
      var task = tasks[t];
      if (!task || !task.id) continue;
      if (!includeArchivedTasks && task.archived) continue;
      taskById[task.id] = task;
    }
    for (var tid in taskById) {
      var tk = taskById[tid];
      nodes.push({
        id: "task:" + tid,
        kind: "task",
        label: tk.title || _shortId(tid),
        status: tk.status || "",
        extra: {
          dispatched: false,
          archived: !!tk.archived,
        },
      });
      // A.depends_on = [B] means B must come before A: emit edge B → A.
      var tdeps = Array.isArray(tk.depends_on) ? tk.depends_on : [];
      for (var td = 0; td < tdeps.length; td++) {
        var targetId = tdeps[td];
        if (!taskById[targetId]) continue;
        edges.push({
          from: "task:" + targetId,
          to: "task:" + tid,
          kind: "task_dep",
        });
      }
    }

    // Leaf spec → task dispatch edges. Flip the `dispatched` flag on the
    // corresponding task node so the renderer can style it distinctly.
    for (var ldPath in specByPath) {
      if (hiddenSpecs[ldPath]) continue;
      var leafSpec = specByPath[ldPath].spec;
      var dtid =
        leafSpec.dispatched_task_id &&
        leafSpec.dispatched_task_id !== "null"
          ? leafSpec.dispatched_task_id
          : null;
      if (!dtid) continue;
      if (!taskById[dtid]) continue;
      edges.push({
        from: "spec:" + ldPath,
        to: "task:" + dtid,
        kind: "dispatch",
      });
      // Mark the task as dispatched-from-a-spec.
      for (var ni = 0; ni < nodes.length; ni++) {
        if (nodes[ni].id === "task:" + dtid) {
          nodes[ni].extra.dispatched = true;
          break;
        }
      }
    }

    return { nodes: nodes, edges: edges };
  }

  function _basename(path) {
    if (!path) return "";
    var idx = path.lastIndexOf("/");
    return idx === -1 ? path : path.substr(idx + 1);
  }

  function _shortId(id) {
    return id && id.length > 8 ? id.substr(0, 8) : id || "";
  }

  // ---------------------------------------------------------------------------
  // Layout + SVG rendering
  // ---------------------------------------------------------------------------

  var PAD = 24;
  var NODE_W = 200;
  var NODE_H = 52;
  var H_GAP = 80;
  var V_GAP = 18;

  // Dummy-node rail: intermediate waypoints for long edges take up far less
  // vertical space than full nodes so the resulting polyline reads as a
  // smooth curve rather than as discrete stepped segments.
  var DUMMY_H = 0;

  // Task status → background colour (matches depgraph.js).
  var TASK_STATUS_COLORS = {
    backlog: "#6b6560",
    in_progress: "#2c5f98",
    waiting: "#a07020",
    committing: "#5a3d8a",
    done: "#1a6030",
    failed: "#8c2020",
    cancelled: "#5a3d8a",
  };

  // Spec status → background colour. Specs use a separate palette so they
  // read as a different node kind at a glance.
  var SPEC_STATUS_COLORS = {
    vague: "#5d4f42",
    drafted: "#6f5632",
    validated: "#2f5a4c",
    complete: "#2a4a2a",
    stale: "#5a3030",
    archived: "#3d3a36",
  };

  // Edge styling per kind. containment and dispatch are structural so they
  // read as thin solid lines; spec_dep and task_dep are design/runtime
  // dependencies so they use dashed variants with distinct hues.
  //
  // task_dep has a secondary state — when the prerequisite task is already
  // done, the edge goes solid to signal "this dependency is satisfied."
  // See _edgeStyle() which consults the source node's status.
  //
  // NOTE: these colours are duplicated in ui/css/docs.css for the header
  // legend (`.depgraph-mode__legend-edge--*`). Keep the two in sync when
  // tweaking the palette; a future theme refactor should consolidate them.
  var EDGE_STYLES = {
    containment: { color: "#7a7570", width: 1.2, dash: null },
    dispatch:    { color: "#3b82c4", width: 1.5, dash: null },
    spec_dep:    { color: "#b07045", width: 1.5, dash: "6 3" },
    task_dep:    { color: "#5a9058", width: 1.5, dash: "4 2" },
  };

  function svgNs(tag) {
    return document.createElementNS("http://www.w3.org/2000/svg", tag);
  }

  function clearChildren(el) {
    while (el.firstChild) el.removeChild(el.firstChild);
  }

  // ---------------------------------------------------------------------------
  // Sugiyama-style layout
  //
  // The layout engine follows the classic Sugiyama framework:
  //   1. Assign a layer (column) to every node via longest-path Kahn so
  //      every predecessor sits strictly to the left.
  //   2. Insert dummy waypoints so every edge spans exactly one layer;
  //      long edges become chains of dummies that sweep-based crossing
  //      minimisation can reorder alongside real nodes.
  //   3. Minimise crossings by alternating top-down and bottom-up sweeps
  //      that reorder nodes within each layer by the barycenter (mean
  //      index) of their neighbours in the adjacent layer.
  //   4. Assign coordinates from the final per-layer ordering, centring
  //      short columns inside taller ones so the graph reads balanced.
  //   5. Route each real edge through its dummy waypoints so long edges
  //      render as smooth curves that thread around intermediate layers
  //      instead of cutting through unrelated clusters.
  // ---------------------------------------------------------------------------

  var BARYCENTER_ITERATIONS = 24;

  function layoutSugiyama(graph) {
    var nodeById = new Map();
    graph.nodes.forEach(function (n) {
      nodeById.set(n.id, n);
    });

    var layerOf = _assignLayers(graph, nodeById);
    var cycleNodes = graph.nodes.filter(function (n) {
      return !layerOf.has(n.id);
    });
    var maxLayer = -1;
    layerOf.forEach(function (L) {
      if (L > maxLayer) maxLayer = L;
    });
    cycleNodes.forEach(function (n) {
      layerOf.set(n.id, maxLayer + 1);
    });

    var layers = _groupByLayer(graph.nodes, layerOf);
    var expanded = _insertDummies(graph, layerOf, layers);

    _minimizeCrossings(expanded.layers, expanded.adjDown, expanded.adjUp);

    var positions = _assignCoordinates(expanded.layers);
    var edgePaths = _routeEdges(graph, expanded.edgeChains, positions);

    var svgW =
      expanded.layers.length > 0
        ? PAD +
          expanded.layers.length * NODE_W +
          Math.max(0, expanded.layers.length - 1) * H_GAP +
          PAD
        : PAD * 2;
    var svgH = _layoutHeight(expanded.layers) + PAD * 2;

    return {
      positions: positions,
      edgePaths: edgePaths,
      svgW: svgW,
      svgH: svgH,
      hasCycles: cycleNodes.length > 0,
    };
  }

  function _assignLayers(graph, nodeById) {
    var inDegree = new Map();
    var adj = new Map();
    graph.nodes.forEach(function (n) {
      inDegree.set(n.id, 0);
      adj.set(n.id, []);
    });
    graph.edges.forEach(function (e) {
      if (!nodeById.has(e.from) || !nodeById.has(e.to)) return;
      inDegree.set(e.to, (inDegree.get(e.to) || 0) + 1);
      adj.get(e.from).push(e.to);
    });
    var layer = new Map();
    var queue = [];
    inDegree.forEach(function (deg, id) {
      if (deg === 0) {
        queue.push(id);
        layer.set(id, 0);
      }
    });
    while (queue.length > 0) {
      var id = queue.shift();
      var myLayer = layer.get(id);
      (adj.get(id) || []).forEach(function (neighbor) {
        var current = layer.has(neighbor) ? layer.get(neighbor) : -1;
        var want = myLayer + 1;
        if (want > current) layer.set(neighbor, want);
        var newDeg = inDegree.get(neighbor) - 1;
        inDegree.set(neighbor, newDeg);
        if (newDeg === 0) queue.push(neighbor);
      });
    }
    return layer;
  }

  function _groupByLayer(nodes, layerOf) {
    var layers = [];
    nodes.forEach(function (n) {
      var L = layerOf.has(n.id) ? layerOf.get(n.id) : 0;
      while (layers.length <= L) layers.push([]);
      layers[L].push({ id: n.id, node: n, kind: "real" });
    });
    return layers;
  }

  function _insertDummies(graph, layerOf, layers) {
    var dummyCounter = 0;
    var expandedLayers = layers.map(function (L) {
      return L.slice();
    });
    var adjDown = new Map();
    var adjUp = new Map();
    var edgeChains = [];

    function ensure(id, bucket) {
      if (!bucket.has(id)) bucket.set(id, []);
    }
    graph.nodes.forEach(function (n) {
      ensure(n.id, adjDown);
      ensure(n.id, adjUp);
    });

    graph.edges.forEach(function (edge) {
      var fromL = layerOf.get(edge.from);
      var toL = layerOf.get(edge.to);
      if (fromL === undefined || toL === undefined || toL <= fromL) {
        edgeChains.push({ edge: edge, chain: [edge.from, edge.to] });
        return;
      }
      var chain = [edge.from];
      var prev = edge.from;
      for (var L = fromL + 1; L < toL; L++) {
        var did = "__dummy_" + dummyCounter++;
        chain.push(did);
        expandedLayers[L].push({ id: did, node: null, kind: "dummy" });
        ensure(did, adjDown);
        ensure(did, adjUp);
        adjDown.get(prev).push(did);
        adjUp.get(did).push(prev);
        prev = did;
      }
      chain.push(edge.to);
      adjDown.get(prev).push(edge.to);
      adjUp.get(edge.to).push(prev);
      edgeChains.push({ edge: edge, chain: chain });
    });

    return {
      layers: expandedLayers,
      adjDown: adjDown,
      adjUp: adjUp,
      edgeChains: edgeChains,
    };
  }

  function _minimizeCrossings(layers, adjDown, adjUp) {
    // Seed with a deterministic order so re-renders of the same data
    // produce identical layouts.
    layers.forEach(function (layer) {
      layer.sort(function (a, b) {
        if (a.kind !== b.kind) return a.kind === "dummy" ? 1 : -1;
        var an = a.node;
        var bn = b.node;
        if (!an || !bn) return a.id < b.id ? -1 : 1;
        if (an.kind !== bn.kind) return an.kind === "spec" ? -1 : 1;
        return a.id < b.id ? -1 : 1;
      });
    });

    for (var iter = 0; iter < BARYCENTER_ITERATIONS; iter++) {
      for (var l = 1; l < layers.length; l++) {
        _reorderByBarycenter(layers[l], layers[l - 1], adjUp);
      }
      for (var m = layers.length - 2; m >= 0; m--) {
        _reorderByBarycenter(layers[m], layers[m + 1], adjDown);
      }
    }
  }

  function _reorderByBarycenter(target, reference, neighborMap) {
    var refIndex = new Map();
    reference.forEach(function (item, idx) {
      refIndex.set(item.id, idx);
    });
    target.forEach(function (item, origIdx) {
      var neighbors = neighborMap.get(item.id) || [];
      var sum = 0;
      var count = 0;
      neighbors.forEach(function (nId) {
        if (refIndex.has(nId)) {
          sum += refIndex.get(nId);
          count++;
        }
      });
      item._bc = count > 0 ? sum / count : origIdx;
      item._origIdx = origIdx;
    });
    target.sort(function (a, b) {
      if (a._bc !== b._bc) return a._bc - b._bc;
      return a._origIdx - b._origIdx;
    });
    target.forEach(function (item) {
      delete item._bc;
      delete item._origIdx;
    });
  }

  function _assignCoordinates(layers) {
    var positions = new Map();
    var rowHeights = layers.map(function (layer) {
      return layer.reduce(function (sum, item) {
        return sum + (item.kind === "dummy" ? DUMMY_H : NODE_H) + V_GAP;
      }, 0);
    });
    var maxRowH = rowHeights.reduce(function (acc, h) {
      return Math.max(acc, h);
    }, 0);

    layers.forEach(function (layer, L) {
      var x = PAD + L * (NODE_W + H_GAP);
      var colH = rowHeights[L];
      var y = PAD + Math.max(0, (maxRowH - colH) / 2);
      layer.forEach(function (item) {
        if (item.kind === "dummy") {
          positions.set(item.id, {
            x: x + NODE_W / 2,
            y: y,
            kind: "dummy",
          });
          y += DUMMY_H + V_GAP;
          return;
        }
        positions.set(item.id, {
          x: x,
          y: y,
          node: item.node,
          kind: "real",
        });
        y += NODE_H + V_GAP;
      });
    });
    return positions;
  }

  function _layoutHeight(layers) {
    var max = 0;
    layers.forEach(function (layer) {
      var h = layer.reduce(function (sum, item) {
        return sum + (item.kind === "dummy" ? DUMMY_H : NODE_H) + V_GAP;
      }, 0);
      if (h > max) max = h;
    });
    return max;
  }

  function _routeEdges(graph, edgeChains, positions) {
    return edgeChains
      .map(function (entry) {
        var points = [];
        for (var i = 0; i < entry.chain.length; i++) {
          var id = entry.chain[i];
          var p = positions.get(id);
          if (!p) return null;
          if (i === 0) {
            points.push({ x: p.x + NODE_W, y: p.y + NODE_H / 2 });
          } else if (i === entry.chain.length - 1) {
            points.push({ x: p.x, y: p.y + NODE_H / 2 });
          } else {
            points.push({ x: p.x, y: p.y });
          }
        }
        return { edge: entry.edge, points: points };
      })
      .filter(Boolean);
  }

  // _smoothPath returns an SVG path `d` attribute that draws a smooth
  // curve through the given waypoints. For each pair of consecutive
  // points, we use a cubic Bezier whose control points sit one gap-unit
  // horizontally from the endpoints so the curve eases into each node
  // and dummy waypoint. With just two points this reduces to the classic
  // horizontally-symmetric "C-shape" bezier the flat layout used before.
  function _smoothPath(points) {
    if (!points || points.length === 0) return "";
    var d = "M" + points[0].x + "," + points[0].y;
    for (var i = 1; i < points.length; i++) {
      var a = points[i - 1];
      var b = points[i];
      var dx = b.x - a.x;
      // Half of the horizontal distance → smooth S-curve through waypoints.
      var cp = Math.max(20, dx / 2);
      d +=
        " C" +
        (a.x + cp) + "," + a.y +
        " " +
        (b.x - cp) + "," + b.y +
        " " +
        b.x + "," + b.y;
    }
    return d;
  }

  // renderUnifiedGraph draws the {nodes, edges} graph into the given SVG
  // element. Returns true when anything was rendered, false on empty graph.
  //
  // Options:
  //   onToggleSpec(path): invoked when a user clicks the +/- handle on a
  //     spec node that has children. Callers wire this to their collapse
  //     state store and re-render.
  function renderUnifiedGraph(graph, svg, opts) {
    opts = opts || {};
    if (!graph || !Array.isArray(graph.nodes) || graph.nodes.length === 0) {
      clearChildren(svg);
      return false;
    }
    if (!svg) return false;

    var layout = layoutSugiyama(graph);

    svg.setAttribute("viewBox", "0 0 " + layout.svgW + " " + layout.svgH);
    svg.setAttribute("width", layout.svgW);
    svg.setAttribute("height", layout.svgH);

    clearChildren(svg);

    // Index nodes by id so edge styling can consult the source's status
    // (used for the task_dep "satisfied" cue).
    var nodeById = new Map();
    graph.nodes.forEach(function (n) {
      nodeById.set(n.id, n);
    });

    // --- Edges first so nodes sit on top ---
    layout.edgePaths.forEach(function (routed) {
      var e = routed.edge;
      var pts = routed.points;
      if (!pts || pts.length < 2) return;

      var base = EDGE_STYLES[e.kind] || EDGE_STYLES.task_dep;
      var style = { color: base.color, width: base.width, dash: base.dash };
      if (e.kind === "task_dep") {
        var src = nodeById.get(e.from);
        if (src && src.kind === "task" && src.status === "done") {
          style.dash = null;
        }
      }

      var path = svgNs("path");
      path.setAttribute("d", _smoothPath(pts));
      path.setAttribute("stroke", style.color);
      path.setAttribute("stroke-width", String(style.width));
      path.setAttribute("fill", "none");
      if (style.dash) path.setAttribute("stroke-dasharray", style.dash);
      path.setAttribute("data-kind", e.kind);
      svg.appendChild(path);
    });

    // --- Nodes ---
    layout.positions.forEach(function (pos) {
      if (pos.kind === "dummy" || !pos.node) return;
      var n = pos.node;
      var x = pos.x;
      var y = pos.y;

      var g = svgNs("g");
      g.setAttribute("data-kind", n.kind);
      g.setAttribute("data-id", n.id);

      // The main body (rect + type chip + label) navigates on click;
      // for specs with children, a separate toggle handle on the right
      // edge stops event propagation so clicking "+" doesn't also focus
      // the spec in Plan mode.
      var body = svgNs("g");
      body.style.cursor = "pointer";
      _wireClick(body, n);

      var rect = svgNs("rect");
      rect.setAttribute("x", String(x));
      rect.setAttribute("y", String(y));
      rect.setAttribute("width", String(NODE_W));
      rect.setAttribute("height", String(NODE_H));
      // Specs use a larger corner radius to read as rounded rectangles;
      // tasks use a tighter radius for a "card" feel.
      rect.setAttribute("rx", n.kind === "spec" ? "12" : "6");
      rect.setAttribute(
        "fill",
        n.kind === "spec"
          ? SPEC_STATUS_COLORS[n.status] || "#4a4540"
          : TASK_STATUS_COLORS[n.status] || "#4B5563",
      );
      // Outline only for dispatched tasks so the spec→task linkage reads.
      if (n.kind === "task" && n.extra && n.extra.dispatched) {
        rect.setAttribute("stroke", "#3b82c4");
        rect.setAttribute("stroke-width", "1.5");
      }
      body.appendChild(rect);

      // Type chip (top-left corner): a small indicator so you can scan
      // the graph for specs vs tasks without relying on shape alone.
      var chip = svgNs("text");
      chip.setAttribute("x", String(x + 8));
      chip.setAttribute("y", String(y + 14));
      chip.setAttribute("font-size", "10");
      chip.setAttribute("font-family", "system-ui, sans-serif");
      chip.setAttribute("fill", "#ffffffb3");
      chip.textContent = n.kind === "spec" ? "SPEC" : "TASK";
      body.appendChild(chip);

      // Label (truncated to fit, leaves room on the right for the toggle
      // handle when the spec has children).
      var hasToggle =
        n.kind === "spec" && n.extra && n.extra.hasChildren;
      var labelMaxChars = hasToggle ? 22 : 26;
      var rawLabel = n.label || "";
      var truncated =
        rawLabel.length > labelMaxChars
          ? rawLabel.slice(0, labelMaxChars) + "\u2026"
          : rawLabel;
      var labelCenterX = hasToggle
        ? x + (NODE_W - 28) / 2
        : x + NODE_W / 2;
      var label = svgNs("text");
      label.setAttribute("x", String(labelCenterX));
      label.setAttribute("y", String(y + NODE_H / 2 + 10));
      label.setAttribute("text-anchor", "middle");
      label.setAttribute("font-size", "12");
      label.setAttribute("font-family", "system-ui, sans-serif");
      label.setAttribute("fill", "#ffffff");
      label.textContent = truncated;
      body.appendChild(label);

      g.appendChild(body);

      // Toggle handle on the right edge for specs with children.
      if (hasToggle) {
        var handle = _makeToggleHandle(n, x, y, opts);
        if (handle) g.appendChild(handle);
      }

      svg.appendChild(g);
    });

    return true;
  }

  function _wireClick(g, n) {
    g.addEventListener("click", function () {
      if (n.kind === "task") {
        if (typeof window.openTaskModal === "function")
          window.openTaskModal(n.id.replace(/^task:/, ""));
        else if (typeof window.openModal === "function")
          window.openModal(n.id.replace(/^task:/, ""));
      } else if (n.kind === "spec") {
        var path = n.extra && n.extra.path;
        if (!path) return;
        if (typeof window.focusSpec === "function") {
          if (typeof window.switchMode === "function")
            window.switchMode("spec", { persist: true });
          window.focusSpec(path);
        }
      }
    });
  }

  // _makeToggleHandle returns an <g> containing the +/- chip that
  // expands/collapses a spec's children. Click handler stops propagation
  // so the main body's spec-focus click doesn't also fire.
  function _makeToggleHandle(n, x, y, opts) {
    var collapsed = !!(n.extra && n.extra.collapsed);
    var cx = x + NODE_W - 16;
    var cy = y + NODE_H / 2;
    var handle = svgNs("g");
    handle.style.cursor = "pointer";
    handle.setAttribute("data-role", "toggle");
    handle.setAttribute("data-collapsed", collapsed ? "1" : "0");

    var circle = svgNs("circle");
    circle.setAttribute("cx", String(cx));
    circle.setAttribute("cy", String(cy));
    circle.setAttribute("r", "9");
    circle.setAttribute("fill", "#ffffff1a");
    circle.setAttribute("stroke", "#ffffff66");
    circle.setAttribute("stroke-width", "1");
    handle.appendChild(circle);

    var glyph = svgNs("text");
    glyph.setAttribute("x", String(cx));
    glyph.setAttribute("y", String(cy + 4));
    glyph.setAttribute("text-anchor", "middle");
    glyph.setAttribute("font-size", "13");
    glyph.setAttribute("font-family", "system-ui, sans-serif");
    glyph.setAttribute("fill", "#ffffff");
    glyph.textContent = collapsed ? "+" : "\u2212"; // − (minus)
    handle.appendChild(glyph);

    handle.addEventListener("click", function (e) {
      if (e && typeof e.stopPropagation === "function") e.stopPropagation();
      if (typeof opts.onToggleSpec === "function") {
        opts.onToggleSpec(n.extra && n.extra.path);
      }
    });
    return handle;
  }

  // Export for browser (window) and CommonJS-style test harness.
  if (typeof window !== "undefined") {
    window.buildUnifiedGraph = buildUnifiedGraph;
    window.renderUnifiedGraph = renderUnifiedGraph;
  }
  if (typeof module !== "undefined" && module.exports) {
    module.exports = {
      buildUnifiedGraph: buildUnifiedGraph,
      renderUnifiedGraph: renderUnifiedGraph,
    };
  }
})();
