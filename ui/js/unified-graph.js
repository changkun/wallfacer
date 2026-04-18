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

  // opts.pinnedPositions — Map<nodeId, {x, y}> of positions the user has
  // dragged. Pinned nodes are fed back into the barycenter sweeps as
  // fixed anchors so unpinned nodes flow around them (incremental
  // relayout), then have their exact coords re-applied at the end. This
  // gives drag-then-relayout the feel of "only what I didn't touch moves."
  function layoutSugiyama(graph, opts) {
    opts = opts || {};
    var pinnedPositions =
      opts.pinnedPositions && typeof opts.pinnedPositions.get === "function"
        ? opts.pinnedPositions
        : null;

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

    var positions = _assignCoordinates(
      expanded.layers,
      expanded.adjDown,
      expanded.adjUp,
    );
    var edgePaths = _routeEdges(graph, expanded.edgeChains, positions);

    // Apply pinned positions last so user-placed coords win over layout.
    // Pinned positions may shift the overall SVG extents — track the
    // maxima so the canvas still contains every pinned node.
    var maxX = 0;
    var maxY = 0;
    positions.forEach(function (p) {
      if (p.x + NODE_W > maxX) maxX = p.x + NODE_W;
      if (p.y + NODE_H > maxY) maxY = p.y + NODE_H;
    });
    if (pinnedPositions) {
      pinnedPositions.forEach(function (pos, id) {
        var current = positions.get(id);
        if (!current || current.kind !== "real") return;
        current.x = pos.x;
        current.y = pos.y;
        if (pos.x + NODE_W > maxX) maxX = pos.x + NODE_W;
        if (pos.y + NODE_H > maxY) maxY = pos.y + NODE_H;
      });
      // Recompute edge paths against the updated positions.
      edgePaths = _routeEdges(graph, expanded.edgeChains, positions);
    }

    var autoW =
      expanded.layers.length > 0
        ? PAD +
          expanded.layers.length * NODE_W +
          Math.max(0, expanded.layers.length - 1) * H_GAP +
          PAD
        : PAD * 2;
    var autoH = _layoutHeight(expanded.layers) + PAD * 2;
    var svgW = Math.max(autoW, maxX + PAD);
    var svgH = Math.max(autoH, maxY + PAD);

    return {
      positions: positions,
      edgePaths: edgePaths,
      svgW: svgW,
      svgH: svgH,
      hasCycles: cycleNodes.length > 0,
    };
  }

  // _assignLayers returns a Map<nodeId, layerIndex>. We use *median*
  // layering: each node's layer is the midpoint of its ASAP (earliest
  // layer it could legally sit in) and ALAP (latest layer it could
  // legally sit in). The midpoint spreads nodes evenly across columns
  // instead of piling every zero-in-degree node at layer 0 — critical
  // when a workspace has many standalone tasks that share "layer 0"
  // with every root spec under longest-path-from-source layering.
  //
  // Edge invariant (for all edges a→b, layer[a] < layer[b]) is preserved
  // because ASAP/ALAP respect the invariant independently, so their
  // midpoints do too (proof: asap[b] ≥ asap[a]+1 and alap[b] ≥ alap[a]+1
  // imply asap[b]+alap[b] ≥ asap[a]+alap[a]+2).
  function _assignLayers(graph, nodeById) {
    var adjDown = new Map();
    var adjUp = new Map();
    graph.nodes.forEach(function (n) {
      adjDown.set(n.id, []);
      adjUp.set(n.id, []);
    });
    graph.edges.forEach(function (e) {
      if (!nodeById.has(e.from) || !nodeById.has(e.to)) return;
      adjDown.get(e.from).push(e.to);
      adjUp.get(e.to).push(e.from);
    });

    // Topological order via DFS. Nodes in cycles get skipped; the caller
    // places them in a trailing cycle column.
    var topo = _topologicalOrder(graph.nodes, adjDown);
    if (!topo) return new Map(); // cycle-only graph; caller handles

    // ASAP: layer[a] = max(layer[pred]+1), default 0.
    var asap = new Map();
    topo.forEach(function (id) {
      var preds = adjUp.get(id) || [];
      var m = 0;
      for (var i = 0; i < preds.length; i++) {
        if (!asap.has(preds[i])) continue;
        if (asap.get(preds[i]) + 1 > m) m = asap.get(preds[i]) + 1;
      }
      asap.set(id, m);
    });
    var maxLayer = 0;
    asap.forEach(function (v) {
      if (v > maxLayer) maxLayer = v;
    });

    // ALAP: layer[a] = min(layer[succ]-1), default maxLayer.
    var alap = new Map();
    for (var i = topo.length - 1; i >= 0; i--) {
      var id = topo[i];
      var succs = adjDown.get(id) || [];
      var m = maxLayer;
      var hasSucc = false;
      for (var j = 0; j < succs.length; j++) {
        if (!alap.has(succs[j])) continue;
        hasSucc = true;
        if (alap.get(succs[j]) - 1 < m) m = alap.get(succs[j]) - 1;
      }
      alap.set(id, hasSucc ? m : maxLayer);
    }

    // Median (round toward source so shorter edges are preferred).
    var layer = new Map();
    asap.forEach(function (a, id) {
      var b = alap.has(id) ? alap.get(id) : a;
      layer.set(id, Math.floor((a + b) / 2));
    });
    return layer;
  }

  // _topologicalOrder returns an Array<nodeId> in topological order, or
  // null if the graph has a cycle reachable from this traversal.
  function _topologicalOrder(nodes, adjDown) {
    var visited = new Map(); // 0 = unseen, 1 = on stack, 2 = done
    var order = [];
    var hadCycle = false;
    function visit(id) {
      if (hadCycle) return;
      var state = visited.get(id);
      if (state === 2) return;
      if (state === 1) {
        hadCycle = true;
        return;
      }
      visited.set(id, 1);
      var nexts = adjDown.get(id) || [];
      for (var i = 0; i < nexts.length; i++) visit(nexts[i]);
      visited.set(id, 2);
      order.push(id);
    }
    for (var k = 0; k < nodes.length; k++) visit(nodes[k].id);
    if (hadCycle) return null;
    order.reverse();
    return order;
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

  // _assignCoordinates turns the per-layer ordering into (x, y) coords.
  // We run a two-pass coordinate assignment so edges read straight:
  //   1. Seed y by the node's position in its layer (naïve top-align).
  //   2. Alternate down/up sweeps that set each node's desired y to the
  //      mean y of its neighbours in the adjacent layer, then resolve
  //      overlaps by packing the column top-to-bottom with at least
  //      NODE_H + V_GAP between successive nodes.
  //
  // The result is that nodes with a shared predecessor cluster at
  // similar heights across columns — edges read as mostly-straight
  // lines instead of zig-zagging between arbitrary slots.
  function _assignCoordinates(layers, adjDown, adjUp) {
    var positions = new Map();
    var STEP = NODE_H + V_GAP;

    function baseY(layer, idx) {
      return PAD + idx * STEP;
    }

    // Seed: top-align each column.
    layers.forEach(function (layer, L) {
      var x = PAD + L * (NODE_W + H_GAP);
      layer.forEach(function (item, idx) {
        positions.set(item.id, {
          x: item.kind === "dummy" ? x + NODE_W / 2 : x,
          y: baseY(L, idx),
          node: item.node || null,
          kind: item.kind,
        });
      });
    });

    if (!adjDown || !adjUp) return positions;

    // Sweep helper: push each node toward the mean y of its neighbours
    // in the *reference* layer, preserving the order in the current
    // layer and packing to prevent overlaps.
    function sweep(layerIdx, refLayerIdx, neighborMap) {
      var layer = layers[layerIdx];
      if (!layer || layer.length === 0) return;
      var refLayer = layers[refLayerIdx];
      if (!refLayer) return;

      var x = positions.get(layer[0].id).x; // column x stays fixed

      // Compute desired y for each node.
      var desired = layer.map(function (item, idx) {
        var ns = neighborMap.get(item.id) || [];
        var sum = 0;
        var count = 0;
        for (var i = 0; i < ns.length; i++) {
          var p = positions.get(ns[i]);
          if (!p) continue;
          sum += p.y;
          count++;
        }
        return {
          item: item,
          idx: idx,
          desired: count > 0 ? sum / count : positions.get(item.id).y,
        };
      });

      // Pack top-to-bottom in the current layer's order (already
      // optimised by barycenter sweeps). For each node, its final y is
      // max(desired, prev_y + STEP).
      var cursor = PAD;
      for (var i = 0; i < desired.length; i++) {
        var d = desired[i];
        var y = Math.max(d.desired, cursor);
        var pos = positions.get(d.item.id);
        pos.y = y;
        cursor = y + (d.item.kind === "dummy" ? DUMMY_H : STEP);
      }
    }

    // Alternate down and up passes; a handful of iterations is enough
    // for the positions to settle on a quasi-fixed point.
    for (var iter = 0; iter < 6; iter++) {
      for (var L = 1; L < layers.length; L++) sweep(L, L - 1, adjUp);
      for (var M = layers.length - 2; M >= 0; M--) sweep(M, M + 1, adjDown);
    }

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
  //   onToggleSpec(path)              — user clicked the +/- handle.
  //   onPinNode(id, x, y)             — user dragged a node; commit the new
  //                                     position to the pin store.
  //   onUnpinNode(id)                 — user double-clicked a pinned node.
  //   onFocusNode(id | null)          — user clicked a node (id) or empty
  //                                     canvas (null) to focus/unfocus.
  //   onNavigateNode(id)              — shift+click navigation.
  //   pinnedIds (Set<string>)         — nodes currently pinned; used to
  //                                     draw the pin corner marker.
  //   focusedNodeId (string | null)   — when set, non-neighbourhood nodes
  //                                     and edges are dimmed so the user
  //                                     can zero in on one topic.
  function renderUnifiedGraph(graph, svg, opts) {
    opts = opts || {};
    if (!graph || !Array.isArray(graph.nodes) || graph.nodes.length === 0) {
      clearChildren(svg);
      return false;
    }
    if (!svg) return false;

    var layout = layoutSugiyama(graph, {
      pinnedPositions: opts.pinnedPositions,
    });

    svg.setAttribute("viewBox", "0 0 " + layout.svgW + " " + layout.svgH);
    svg.setAttribute("width", layout.svgW);
    svg.setAttribute("height", layout.svgH);

    clearChildren(svg);

    var nodeById = new Map();
    graph.nodes.forEach(function (n) {
      nodeById.set(n.id, n);
    });

    // Build a focus neighbourhood so the renderer can dim everything
    // outside of it. Includes the focused node itself plus every direct
    // neighbour reachable along any edge kind (up or down). 1-hop is
    // intentional: 2-hop tends to re-include most of the graph and
    // defeats the "zero in on one topic" goal.
    var focusedId = opts.focusedNodeId || null;
    var focusSet = null;
    if (focusedId && nodeById.has(focusedId)) {
      focusSet = new Set([focusedId]);
      graph.edges.forEach(function (e) {
        if (e.from === focusedId) focusSet.add(e.to);
        if (e.to === focusedId) focusSet.add(e.from);
      });
    }

    var pinnedIds =
      opts.pinnedIds && typeof opts.pinnedIds.has === "function"
        ? opts.pinnedIds
        : null;

    // Let the canvas swallow clicks on empty space → clear focus. Placed
    // as the first child (behind everything) so a click that hits a node
    // still fires the node's handler first.
    var backdrop = svgNs("rect");
    backdrop.setAttribute("x", "0");
    backdrop.setAttribute("y", "0");
    backdrop.setAttribute("width", String(layout.svgW));
    backdrop.setAttribute("height", String(layout.svgH));
    backdrop.setAttribute("fill", "transparent");
    backdrop.setAttribute("data-role", "canvas-backdrop");
    backdrop.addEventListener("click", function () {
      if (typeof opts.onFocusNode === "function") opts.onFocusNode(null);
    });
    svg.appendChild(backdrop);

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
      if (focusSet) {
        var edgeInFocus = focusSet.has(e.from) && focusSet.has(e.to);
        if (!edgeInFocus) path.setAttribute("opacity", "0.18");
      }
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
      if (focusSet && !focusSet.has(n.id)) {
        g.setAttribute("opacity", "0.28");
      }

      var body = svgNs("g");
      body.style.cursor = "grab";

      var rect = svgNs("rect");
      rect.setAttribute("x", String(x));
      rect.setAttribute("y", String(y));
      rect.setAttribute("width", String(NODE_W));
      rect.setAttribute("height", String(NODE_H));
      rect.setAttribute("rx", n.kind === "spec" ? "12" : "6");
      rect.setAttribute(
        "fill",
        n.kind === "spec"
          ? SPEC_STATUS_COLORS[n.status] || "#4a4540"
          : TASK_STATUS_COLORS[n.status] || "#4B5563",
      );
      if (n.kind === "task" && n.extra && n.extra.dispatched) {
        rect.setAttribute("stroke", "#3b82c4");
        rect.setAttribute("stroke-width", "1.5");
      }
      if (focusedId === n.id) {
        rect.setAttribute("stroke", "#f7c466");
        rect.setAttribute("stroke-width", "2");
      }
      body.appendChild(rect);

      var chip = svgNs("text");
      chip.setAttribute("x", String(x + 8));
      chip.setAttribute("y", String(y + 14));
      chip.setAttribute("font-size", "10");
      chip.setAttribute("font-family", "system-ui, sans-serif");
      chip.setAttribute("fill", "#ffffffb3");
      chip.textContent = n.kind === "spec" ? "SPEC" : "TASK";
      body.appendChild(chip);

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

      // Pin marker on the top-left corner for user-pinned nodes.
      if (pinnedIds && pinnedIds.has(n.id)) {
        var pin = svgNs("circle");
        pin.setAttribute("cx", String(x + 4));
        pin.setAttribute("cy", String(y + 4));
        pin.setAttribute("r", "3");
        pin.setAttribute("fill", "#f7c466");
        body.appendChild(pin);
      }

      _wireNodeInteractions(g, body, n, x, y, opts);
      g.appendChild(body);

      if (hasToggle) {
        var handle = _makeToggleHandle(n, x, y, opts);
        if (handle) g.appendChild(handle);
      }

      svg.appendChild(g);
    });

    return true;
  }

  // _wireNodeInteractions binds drag / click / dblclick / shift+click on
  // a single node group. The drag state machine lives here so it can
  // distinguish drags from clicks via a movement threshold, preventing
  // a normal click from triggering both focus and pin.
  var DRAG_THRESHOLD = 4;

  function _wireNodeInteractions(g, body, n, x, y, opts) {
    var dragState = null;

    body.addEventListener("mousedown", function (e) {
      // Let space+drag (canvas pan) win over node drag so the user can
      // pan over a node. Also ignore right-click / middle-click.
      if (e.button !== undefined && e.button !== 0) return;
      dragState = {
        startX: e.clientX,
        startY: e.clientY,
        originX: x,
        originY: y,
        moved: false,
      };
      body.style.cursor = "grabbing";
      // Stop propagation so the SVG backdrop's click handler doesn't
      // fire on mouseup-after-drag.
      if (typeof e.stopPropagation === "function") e.stopPropagation();
    });

    body.addEventListener("mousemove", function (e) {
      if (!dragState) return;
      var dx = e.clientX - dragState.startX;
      var dy = e.clientY - dragState.startY;
      if (
        !dragState.moved &&
        Math.hypot(dx, dy) < DRAG_THRESHOLD
      ) {
        return;
      }
      dragState.moved = true;
      g.setAttribute("transform", "translate(" + dx + "," + dy + ")");
    });

    function finishDrag(e) {
      if (!dragState) return;
      var wasMoved = dragState.moved;
      var dx = e.clientX - dragState.startX;
      var dy = e.clientY - dragState.startY;
      dragState = null;
      body.style.cursor = "grab";
      if (!wasMoved) return;
      g.removeAttribute("transform");
      if (typeof opts.onPinNode === "function") {
        opts.onPinNode(n.id, x + dx, y + dy);
      }
    }

    body.addEventListener("mouseup", finishDrag);
    // If the mouse leaves the body mid-drag, fall back to a document
    // mouseup so we don't lose the drag release.
    body.addEventListener("mouseleave", function () {
      if (!dragState) return;
      var releasedOnce = false;
      var onDocUp = function (e2) {
        if (releasedOnce) return;
        releasedOnce = true;
        document.removeEventListener("mouseup", onDocUp);
        finishDrag(e2);
      };
      document.addEventListener("mouseup", onDocUp);
    });

    body.addEventListener("click", function (e) {
      // A click fires after mouseup; suppress it if the mouseup was the
      // tail end of a drag.
      if (g.getAttribute("transform")) return;
      if (e.shiftKey) {
        if (typeof opts.onNavigateNode === "function") {
          opts.onNavigateNode(n.id, n);
          return;
        }
      }
      if (typeof opts.onFocusNode === "function") opts.onFocusNode(n.id);
    });

    body.addEventListener("dblclick", function (e) {
      if (typeof e.stopPropagation === "function") e.stopPropagation();
      if (typeof opts.onUnpinNode === "function") opts.onUnpinNode(n.id);
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
