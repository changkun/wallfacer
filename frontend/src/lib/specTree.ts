/* Helpers for the spec tree shown by PlanPage. Extracted from the
   component so the visibility computation can be unit-tested. */

export interface SpecNodeLite {
  path: string;
  children?: string[] | null;
}

/* Compute the set of paths hidden because an ancestor is collapsed.
   Defensive against missing/null `children` (the /api/specs/tree
   response omits the field for leaf nodes, which previously caused
   a TypeError when reading length on undefined/null). */
export function computeHidden<T extends SpecNodeLite>(
  sortedNodes: readonly T[],
  collapsed: ReadonlySet<string>,
  index: ReadonlyMap<string, T>,
): Set<string> {
  const hidden = new Set<string>();
  for (const node of sortedNodes) {
    if (hidden.has(node.path)) continue;
    const children = node.children ?? [];
    if (collapsed.has(node.path) && children.length > 0) {
      const queue = [...children];
      while (queue.length) {
        const p = queue.shift()!;
        hidden.add(p);
        const child = index.get(p);
        if (child) queue.push(...(child.children ?? []));
      }
    }
  }
  return hidden;
}
