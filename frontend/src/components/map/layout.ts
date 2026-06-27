// Layered (left-to-right) layout for the unified spec+task graph.
//
// This is the hierarchy/overlap fix (bug #1): instead of arbitrary positions
// with straight edges crossing each other, nodes are assigned to dependency
// layers (column = longest path from a root) and spread vertically within a
// layer, so the flow of work reads left→right and nodes never share a slot.
//
// Pure and deterministic: same graph in → same coordinates out, which is what
// makes it unit-testable without a DOM.

import type { Graph } from '../../api/types';

export interface Point {
  x: number;
  y: number;
}

export interface LayoutOptions {
  colWidth?: number; // horizontal gap between layers
  rowHeight?: number; // vertical gap between nodes in a layer
  originX?: number;
  originY?: number;
}

const DEFAULTS: Required<LayoutOptions> = {
  colWidth: 220,
  rowHeight: 90,
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

  // Group by layer in stable node order, then assign rows within each column.
  const byLayer = new Map<number, string[]>();
  for (const id of ids) {
    const l = layer.get(id)!;
    if (!byLayer.has(l)) byLayer.set(l, []);
    byLayer.get(l)!.push(id);
  }

  const pos = new Map<string, Point>();
  for (const [l, members] of byLayer) {
    members.forEach((id, row) => {
      pos.set(id, {
        x: o.originX + l * o.colWidth,
        y: o.originY + row * o.rowHeight,
      });
    });
  }
  return pos;
}

// layerCount is the number of distinct columns, useful for sizing the canvas.
export function layerCount(pos: Map<string, Point>, colWidth = DEFAULTS.colWidth, originX = DEFAULTS.originX): number {
  let max = 0;
  for (const p of pos.values()) max = Math.max(max, Math.round((p.x - originX) / colWidth));
  return pos.size === 0 ? 0 : max + 1;
}
