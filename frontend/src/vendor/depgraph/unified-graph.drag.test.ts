import { describe, it, expect } from 'vitest';
// The vendored module exports its internals via module.exports, which
// vitest's CJS interop surfaces on the namespace import.
// @ts-ignore plain ES5-ish JS, no type declarations.
import * as graphMod from './unified-graph.js';

const renderUnifiedGraph = (graphMod as any).renderUnifiedGraph;

function node(id: string, label: string) {
  return { id, kind: 'task', label, status: 'backlog', extra: {} };
}
function edge(from: string, to: string, kind = 'task_dep') {
  return { from, to, kind };
}

// Find the inner draggable <g> (body) for a node: it's the child group of
// the node's data-id group that carries the mousedown/mousemove listeners.
function nodeBody(svg: SVGSVGElement, id: string): SVGGElement {
  const group = svg.querySelector(`g[data-id="${id}"]`) as SVGGElement;
  return group.querySelector('g') as SVGGElement;
}

function edgePath(svg: SVGSVGElement, from: string, to: string): SVGPathElement {
  return svg.querySelector(
    `path[data-from="${from}"][data-to="${to}"]`,
  ) as SVGPathElement;
}

function dispatchMouse(el: Element, type: string, x: number, y: number) {
  el.dispatchEvent(
    new MouseEvent(type, { bubbles: true, clientX: x, clientY: y, button: 0 }),
  );
}

describe('unified-graph node drag re-routes incident edges live', () => {
  // a -> b -> c chain plus a long a -> c edge that spans two layers, so the
  // layout routes it through a dummy waypoint (a 3-point edge). Dragging a
  // must move BOTH the direct 2-point edge (a->b) and the dummy-routed
  // multi-point edge (a->c) at the same time.
  function setup() {
    const nodes = [node('a', 'A'), node('b', 'B'), node('c', 'C')];
    const edges = [edge('a', 'b'), edge('b', 'c'), edge('a', 'c')];
    const svg = document.createElementNS(
      'http://www.w3.org/2000/svg',
      'svg',
    ) as SVGSVGElement;
    document.body.appendChild(svg);
    const ok = renderUnifiedGraph({ nodes, edges }, svg, {});
    expect(ok).not.toBe(false);
    return svg;
  }

  it('updates a multi-point (dummy-routed) edge during the drag, not only on drag-end', () => {
    const svg = setup();
    const ac = edgePath(svg, 'a', 'c');
    // Sanity: a->c is genuinely a multi-point edge (>2 polyline points).
    const acPoints = (ac.getAttribute('d') || '').match(/[ML]/g) || [];
    expect(acPoints.length).toBeGreaterThan(2);
    const before = ac.getAttribute('d');

    const body = nodeBody(svg, 'a');
    dispatchMouse(body, 'mousedown', 0, 0);
    dispatchMouse(body, 'mousemove', 80, 60); // well past DRAG_THRESHOLD

    // The arrow must follow the node mid-drag (this is the reported bug:
    // the multi-point edge stayed frozen until drag-end).
    expect(ac.getAttribute('d')).not.toBe(before);
  });

  it('keeps both endpoints when one node is dragged after another (pins do not re-render)', () => {
    const svg = setup();
    const ac = edgePath(svg, 'a', 'c');

    // Drag a, release.
    const bodyA = nodeBody(svg, 'a');
    dispatchMouse(bodyA, 'mousedown', 0, 0);
    dispatchMouse(bodyA, 'mousemove', 80, 60);
    dispatchMouse(bodyA, 'mouseup', 80, 60);
    const afterA = ac.getAttribute('d') || '';
    const startAfterA = afterA.split(' ')[0]; // the "M…" start point (a's end)

    // Now drag c. a's endpoint must stay put — the shared polyline must not
    // reset a's start point back to its original position.
    const bodyC = nodeBody(svg, 'c');
    dispatchMouse(bodyC, 'mousedown', 0, 0);
    dispatchMouse(bodyC, 'mousemove', 80, 60);
    const afterC = ac.getAttribute('d') || '';
    const startAfterC = afterC.split(' ')[0];

    expect(startAfterC).toBe(startAfterA);
  });

  it('still updates a direct 2-point edge during the drag', () => {
    const svg = setup();
    const ab = edgePath(svg, 'a', 'b');
    const before = ab.getAttribute('d');

    const body = nodeBody(svg, 'a');
    dispatchMouse(body, 'mousedown', 0, 0);
    dispatchMouse(body, 'mousemove', 80, 60);

    expect(ab.getAttribute('d')).not.toBe(before);
  });
});
