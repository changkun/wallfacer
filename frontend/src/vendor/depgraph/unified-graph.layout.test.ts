import { describe, it, expect } from 'vitest';
// The vendored module exports its layout internals via module.exports,
// which vitest's CJS interop surfaces on the default/namespace import.
// @ts-ignore plain ES5-ish JS, no type declarations.
import * as graphMod from './unified-graph.js';

const layoutSugiyama = (graphMod as any).layoutSugiyama;
const NODE_W: number = (graphMod as any).NODE_W;
const MIN_NODE_GAP: number = (graphMod as any).MIN_NODE_GAP;

// node helper. Varies label length so node heights differ (label wrap
// grows height), which is what the overlap pass must account for.
function node(id: string, label: string, kind: 'spec' | 'task' = 'task') {
  return { id, kind, label, status: kind === 'spec' ? 'drafted' : 'backlog', extra: {} };
}
function edge(from: string, to: string, kind = 'task_dep') {
  return { from, to, kind };
}

// rectsOverlap returns true if two real-node bounding boxes are closer
// than the guaranteed MIN_NODE_GAP on BOTH axes (i.e. they overlap or
// crowd). Uses each node's actual computed width (NODE_W) and height.
function tooClose(
  a: { x: number; y: number; w: number; h: number },
  b: { x: number; y: number; w: number; h: number },
  gap: number,
): boolean {
  const sepX = a.x + a.w + gap <= b.x || b.x + b.w + gap <= a.x;
  const sepY = a.y + a.h + gap <= b.y || b.y + b.h + gap <= a.y;
  // Crowded only if NOT separated on either axis. A pure strict-overlap
  // check uses gap=0; crowding uses gap=MIN_NODE_GAP minus a slack.
  return !sepX && !sepY;
}

function realBoxes(layout: any) {
  const boxes: { id: string; x: number; y: number; w: number; h: number }[] = [];
  layout.positions.forEach((p: any, id: string) => {
    // Skip dummy waypoints: they carry no node and zero height.
    if (!p.node || p.kind === 'dummy') return;
    boxes.push({ id, x: p.x, y: p.y, w: NODE_W, h: p.height });
  });
  return boxes;
}

describe('unified-graph layout', () => {
  it('exports the layout function and constants', () => {
    expect(typeof layoutSugiyama).toBe('function');
    expect(NODE_W).toBeGreaterThan(0);
    expect(MIN_NODE_GAP).toBeGreaterThan(0);
  });

  it('produces no overlapping node rectangles across multiple components and varied labels', () => {
    // Component A: a small DAG with short and long (wrapping) labels.
    // Component B: a chain. Component C: an isolated node. Plus several
    // standalone tasks so the within-layer packing is exercised.
    const nodes = [
      node('a1', 'root spec with a deliberately long label that wraps across several lines', 'spec'),
      node('a2', 'child', 'spec'),
      node('a3', 'another child with medium length label here', 'spec'),
      node('a4', 'a4'),
      node('a5', 'a fairly long task title that also wraps to two lines'),
      // Component B
      node('b1', 'beta one'),
      node('b2', 'beta two with a longer label that wraps'),
      node('b3', 'b3'),
      // Component C: isolated
      node('c1', 'isolated node'),
      // standalone tasks (each its own component)
      node('s1', 's1'),
      node('s2', 'standalone task two'),
      node('s3', 's3'),
      node('s4', 'standalone task four with longer text'),
      node('s5', 's5'),
    ];
    const edges = [
      edge('a1', 'a2', 'containment'),
      edge('a1', 'a3', 'containment'),
      edge('a2', 'a4'),
      edge('a3', 'a5'),
      edge('a4', 'a5'),
      edge('b1', 'b2'),
      edge('b2', 'b3'),
    ];

    const layout = layoutSugiyama({ nodes, edges }, {});
    const boxes = realBoxes(layout);
    expect(boxes.length).toBe(nodes.length);

    // All coordinates finite and on-canvas.
    for (const b of boxes) {
      expect(Number.isFinite(b.x)).toBe(true);
      expect(Number.isFinite(b.y)).toBe(true);
      expect(Number.isFinite(b.h)).toBe(true);
      expect(b.x).toBeGreaterThanOrEqual(0);
      expect(b.y).toBeGreaterThanOrEqual(0);
    }

    // Core guarantee: no two real node rectangles overlap. Use a small
    // slack below MIN_NODE_GAP to tolerate rounding (positions are
    // rounded to integers) while still proving non-overlap.
    const slack = 1;
    for (let i = 0; i < boxes.length; i++) {
      for (let j = i + 1; j < boxes.length; j++) {
        const overlap = tooClose(boxes[i], boxes[j], 0);
        expect(
          overlap,
          `nodes ${boxes[i].id} and ${boxes[j].id} overlap: ` +
            JSON.stringify(boxes[i]) + ' vs ' + JSON.stringify(boxes[j]),
        ).toBe(false);
        // Stronger: respect the minimum gap (minus rounding slack).
        const crowded = tooClose(boxes[i], boxes[j], MIN_NODE_GAP - slack);
        expect(
          crowded,
          `nodes ${boxes[i].id} and ${boxes[j].id} crowd below MIN_NODE_GAP`,
        ).toBe(false);
      }
    }

    // Canvas dimensions are finite and positive.
    expect(layout.svgW).toBeGreaterThan(0);
    expect(layout.svgH).toBeGreaterThan(0);
  });

  it('packs many disconnected components into a balanced canvas (not a tall column)', () => {
    // 12 isolated nodes should pack into a grid, so the canvas is wider
    // than a single column stack would be and shorter than 12 stacked
    // rows. We assert the aspect ratio is not extreme.
    const nodes = [];
    for (let i = 0; i < 12; i++) nodes.push(node('iso' + i, 'iso ' + i));
    const layout = layoutSugiyama({ nodes, edges: [] }, {});

    const boxes = realBoxes(layout);
    expect(boxes.length).toBe(12);
    // No overlaps.
    for (let i = 0; i < boxes.length; i++) {
      for (let j = i + 1; j < boxes.length; j++) {
        expect(tooClose(boxes[i], boxes[j], 0)).toBe(false);
      }
    }
    // Multiple distinct columns (x values) → grid, not a column.
    const xs = new Set(boxes.map((b) => b.x));
    expect(xs.size).toBeGreaterThan(1);
    // Multiple rows too.
    const ys = new Set(boxes.map((b) => b.y));
    expect(ys.size).toBeGreaterThan(1);
  });
});
