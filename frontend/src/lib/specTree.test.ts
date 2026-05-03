import { describe, it, expect } from 'vitest';
import { computeHidden, type SpecNodeLite } from './specTree';

interface Node extends SpecNodeLite { id?: string }

function indexOf(nodes: readonly Node[]) {
  return new Map(nodes.map(n => [n.path, n] as const));
}

describe('computeHidden', () => {
  it('hides descendants of a collapsed node', () => {
    const nodes: Node[] = [
      { path: 'root', children: ['root/a', 'root/b'] },
      { path: 'root/a', children: ['root/a/x'] },
      { path: 'root/a/x', children: [] },
      { path: 'root/b', children: [] },
    ];
    const hidden = computeHidden(nodes, new Set(['root/a']), indexOf(nodes));
    expect([...hidden].sort()).toEqual(['root/a/x']);
  });

  it('cascades hidden state through nested collapsed ancestors', () => {
    const nodes: Node[] = [
      { path: 'root', children: ['root/a'] },
      { path: 'root/a', children: ['root/a/x'] },
      { path: 'root/a/x', children: [] },
    ];
    const hidden = computeHidden(nodes, new Set(['root']), indexOf(nodes));
    expect([...hidden].sort()).toEqual(['root/a', 'root/a/x']);
  });

  // Regression: nodes from /api/specs/tree may have undefined or null
  // children when there are none. Reading .length on those threw a
  // TypeError that blanked the entire Plan page.
  it('treats missing children as an empty list', () => {
    const nodes: Node[] = [
      { path: 'leaf-undefined' } as Node,
      { path: 'leaf-null', children: null },
      { path: 'has-null-child', children: ['leaf-null'] },
    ];
    expect(() => computeHidden(nodes, new Set(['leaf-undefined']), indexOf(nodes))).not.toThrow();
    expect(() => computeHidden(nodes, new Set(['leaf-null']), indexOf(nodes))).not.toThrow();
    // Collapsing a parent whose child has null children must still hide
    // the child without crashing on the descendant traversal.
    const hidden = computeHidden(nodes, new Set(['has-null-child']), indexOf(nodes));
    expect([...hidden]).toEqual(['leaf-null']);
  });

  it('returns an empty set when nothing is collapsed', () => {
    const nodes: Node[] = [
      { path: 'a', children: ['a/x'] },
      { path: 'a/x', children: [] },
    ];
    const hidden = computeHidden(nodes, new Set(), indexOf(nodes));
    expect(hidden.size).toBe(0);
  });
});
