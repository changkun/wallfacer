import { describe, it, expect } from 'vitest';
import { computeLayout } from './layout';
import type { Graph, GraphNode } from '../../api/types';

function node(id: string): GraphNode {
  return { id, kind: 'spec', label: id, status: 'drafted', ref: id, depth: 0 };
}

// a → b, a → c, b → d : d depends (transitively) on a, so it must sit in a
// later column than a and b.
function diamondish(): Graph {
  return {
    nodes: ['a', 'b', 'c', 'd'].map(node),
    edges: [
      { from: 'a', to: 'b', kind: 'spec_dep' },
      { from: 'a', to: 'c', kind: 'spec_dep' },
      { from: 'b', to: 'd', kind: 'spec_dep' },
    ],
    critical_path: [],
    blocked: [],
  };
}

describe('computeLayout', () => {
  it('places every node at a distinct position (no overlap)', () => {
    const pos = computeLayout(diamondish());
    const slots = new Set([...pos.values()].map((p) => `${p.x},${p.y}`));
    expect(slots.size).toBe(pos.size);
    expect(pos.size).toBe(4);
  });

  it('orders nodes left→right by dependency layer', () => {
    const pos = computeLayout(diamondish());
    const x = (id: string) => pos.get(id)!.x;
    // a is a root (col 0); b,c depend on a; d depends on b.
    expect(x('a')).toBeLessThan(x('b'));
    expect(x('a')).toBeLessThan(x('c'));
    expect(x('b')).toBeLessThan(x('d'));
    // b and c share a layer → same column.
    expect(x('b')).toBe(x('c'));
  });

  it('terminates on a cycle instead of recursing forever', () => {
    const g: Graph = {
      nodes: ['x', 'y'].map(node),
      edges: [
        { from: 'x', to: 'y', kind: 'task_dep' },
        { from: 'y', to: 'x', kind: 'task_dep' },
      ],
      critical_path: [],
      blocked: [],
    };
    const pos = computeLayout(g);
    expect(pos.size).toBe(2);
  });
});
