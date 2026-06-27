// Layered (left-to-right) layout for the unified spec+task graph.
//
// Hierarchy/overlap fix (bug #1): nodes are assigned to dependency layers
// (column = longest path from a root) so the flow of work reads left→right.
//
// Crucially, a layer is *grid-wrapped*: on the real repo the spec tree has
// layers of 90+ independent nodes, and stacking those in one column produces an
// 8000px-tall wall — exactly the "still a mess" the rebuild is meant to kill. A
// layer wider than `maxRows` is packed into multiple sub-columns, bounding
// column height to `maxRows * rowHeight`, and each layer's horizontal span is
// accumulated so neighbouring layers never overlap.
//
// Pure and deterministic: same graph in → same coordinates out.

import type { Graph } from '../../api/types';

export interface Point {
  x: number;
  y: number;
}

export interface LayoutOptions {
  colWidth?: number; // horizontal step between sub-columns
  rowHeight?: number; // vertical step between nodes in a sub-column
  layerGap?: number; // extra horizontal gap between dependency layers
  maxRows?: number; // max nodes stacked in one sub-column before wrapping
  originX?: number;
  originY?: number;
}

const DEFAULTS: Required<LayoutOptions> = {
  colWidth: 210,
  rowHeight: 90,
  layerGap: 70,
  maxRows: 16,
  originX: 60,
  originY: 50,
};

// computeLayout returns a node-id → {x,y} map. The layer of a node is the
// longest directed path reaching it (over all edges), so a node always sits to
// the right of everything it depends on / is contained by. Cycles — which a
// well-formed spec tree never has — are guarded so the assignment terminates.
export function computeLayout(graph: Graph, opts: LayoutOptions = {}): Map<string, Point> {
  const o = { ...DEFAULTS, ...opts };
  const ids = graph.nodes.map((n) => n.id);
  const idSet = new Set(ids);

  // Incoming adjacency, restricted to edges between present nodes.
  const incoming = new Map<string, string[]>();
  for (const id of ids) incoming.set(id, []);
  for (const e of graph.edges) {
    if (idSet.has(e.from) && idSet.has(e.to)) {
      incoming.get(e.to)!.push(e.from);
    }
  }

  // layer(n) = 0 if no predecessors, else max(layer(pred)) + 1.
  const layer = new Map<string, number>();
  const visiting = new Set<string>();
  const layerOf = (id: string): number => {
    const cached = layer.get(id);
    if (cached !== undefined) return cached;
    if (visiting.has(id)) return 0; // cycle guard
    visiting.add(id);
    let best = 0;
    for (const pred of incoming.get(id)!) {
      best = Math.max(best, layerOf(pred) + 1);
    }
    visiting.delete(id);
    layer.set(id, best);
    return best;
  };
  for (const id of ids) layerOf(id);

  // Group by layer in stable node order.
  const byLayer = new Map<number, string[]>();
  for (const id of ids) {
    const l = layer.get(id)!;
    if (!byLayer.has(l)) byLayer.set(l, []);
    byLayer.get(l)!.push(id);
  }

  // Walk layers left→right, grid-wrapping each into sub-columns and advancing a
  // running x cursor so a wide layer pushes later layers further right rather
  // than overlapping them.
  const pos = new Map<string, Point>();
  let xCursor = o.originX;
  for (const l of [...byLayer.keys()].sort((a, b) => a - b)) {
    const members = byLayer.get(l)!;
    const subCols = Math.max(1, Math.ceil(members.length / o.maxRows));
    members.forEach((id, i) => {
      const subCol = Math.floor(i / o.maxRows);
      const row = i % o.maxRows;
      pos.set(id, {
        x: xCursor + subCol * o.colWidth,
        y: o.originY + row * o.rowHeight,
      });
    });
    xCursor += subCols * o.colWidth + o.layerGap;
  }
  return pos;
}
