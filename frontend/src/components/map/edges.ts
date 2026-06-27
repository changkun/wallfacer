// Curved edge geometry for the unified graph.
//
// Two jobs:
//  1. Draw a smooth cubic-Bézier between two node centers (the curved-edges +
//     overlap-clarity fix, bug #1) instead of a straight polyline through
//     frozen waypoints.
//  2. Recompute edges purely from *live* node positions every time, so a
//     dragged node's edges re-aim both endpoints and never visually detach
//     (the detachment half of bug #2). There are no cached middle waypoints.

import type { Graph, GraphEdge } from '../../api/types';
import type { Point } from './layout';

// edgePath returns a cubic-Bézier `d` from a→b, biased horizontally to suit the
// left→right layered flow. The endpoints are exactly a and b, so the path is
// always attached to whatever positions it is given.
export function edgePath(a: Point, b: Point): string {
  const dx = b.x - a.x;
  const ctrl = Math.max(40, Math.abs(dx) * 0.5);
  return `M ${a.x} ${a.y} C ${a.x + ctrl} ${a.y} ${b.x - ctrl} ${b.y} ${b.x} ${b.y}`;
}

export interface RenderedEdge extends GraphEdge {
  d: string;
}

// edgePaths recomputes every edge's `d` from the current position map. Edges
// whose endpoints are not both positioned are skipped. Because this reads
// positions live, calling it after a drag flush yields edges attached to the
// node's new location — there is no separate "edge endpoint" state to drift.
export function edgePaths(graph: Graph, pos: Map<string, Point>): RenderedEdge[] {
  const out: RenderedEdge[] = [];
  for (const e of graph.edges) {
    const a = pos.get(e.from);
    const b = pos.get(e.to);
    if (!a || !b) continue;
    out.push({ ...e, d: edgePath(a, b) });
  }
  return out;
}
