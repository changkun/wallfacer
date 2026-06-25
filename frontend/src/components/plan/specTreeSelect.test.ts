import { describe, it, expect } from 'vitest';
import type { SpecNode } from '../../stores/agentSession';
import { isNodeCheckable, nodeUnmetDeps, selectableRange } from './specTreeSelect';

function node(path: string, status: string, depends_on: string[] = []): SpecNode {
  return {
    path,
    spec: { status, depends_on, title: path },
    children: [],
    is_leaf: true,
    depth: 0,
  };
}

function mapOf(nodes: SpecNode[]): Map<string, SpecNode> {
  const m = new Map<string, SpecNode>();
  for (const n of nodes) m.set(n.path, n);
  return m;
}

describe('isNodeCheckable', () => {
  it('only validated specs are checkable', () => {
    expect(isNodeCheckable(node('a', 'validated'))).toBe(true);
    expect(isNodeCheckable(node('a', 'drafted'))).toBe(false);
    expect(isNodeCheckable(undefined)).toBe(false);
  });
});

describe('nodeUnmetDeps', () => {
  it('returns titles of incomplete dependencies', () => {
    const dep = node('dep', 'validated');
    const n = node('a', 'validated', ['dep']);
    expect(nodeUnmetDeps(n, mapOf([dep, n]))).toEqual(['dep']);
  });
  it('is empty when all dependencies are complete', () => {
    const dep = node('dep', 'complete');
    const n = node('a', 'validated', ['dep']);
    expect(nodeUnmetDeps(n, mapOf([dep, n]))).toEqual([]);
  });
});

describe('selectableRange', () => {
  it('skips non-validated and dependency-blocked nodes in the sweep', () => {
    const validated = node('ok', 'validated');
    const drafted = node('draft', 'drafted');
    const blockedDep = node('dep', 'validated');
    const blocked = node('blocked', 'validated', ['dep']);
    const nodes = [validated, drafted, blocked, blockedDep];
    const paths = ['ok', 'draft', 'blocked', 'dep'];
    // Sweep the whole rendered range. Only `ok` and `dep` are selectable;
    // `draft` is not validated and `blocked` has an incomplete dependency.
    const out = selectableRange(paths, mapOf(nodes), 0, 3);
    expect(out).toEqual(['ok', 'dep']);
  });
  it('is order-independent for the index bounds', () => {
    const a = node('a', 'validated');
    const b = node('b', 'validated');
    const paths = ['a', 'b'];
    const m = mapOf([a, b]);
    expect(selectableRange(paths, m, 1, 0)).toEqual(['a', 'b']);
  });
});
