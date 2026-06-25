// Pure selection helpers for SpecTreePanel multi-select dispatch. Extracted
// so the checkable/blocked predicates can be unit-tested without driving a
// component (shiftKey events are awkward to simulate).

import type { SpecNode } from '../../stores/planning';

/** A spec node is checkable for dispatch only when validated. */
export function isNodeCheckable(node: SpecNode | undefined): boolean {
  return node?.spec?.status === 'validated';
}

/** Titles (or paths) of dependencies that are not yet complete. Empty when
 *  the node is unblocked. */
export function nodeUnmetDeps(
  node: SpecNode | undefined,
  byPath: Map<string, SpecNode>,
): string[] {
  const deps = node?.spec?.depends_on ?? [];
  if (deps.length === 0) return [];
  const out: string[] = [];
  for (const dp of deps) {
    const dn = byPath.get(dp);
    if (!dn || dn.spec?.status !== 'complete') {
      out.push(dn?.spec?.title ?? dp);
    }
  }
  return out;
}

/** Resolve the inclusive index range [a,b] (order-independent) to the paths
 *  that are actually selectable: checkable (validated) and unblocked (no
 *  unmet deps). Mirrors the checkbox template gating so shift-range selection
 *  cannot sweep in non-validated or dependency-blocked specs. */
export function selectableRange(
  paths: string[],
  byPath: Map<string, SpecNode>,
  a: number,
  b: number,
): string[] {
  const start = Math.min(a, b);
  const end = Math.max(a, b);
  const out: string[] = [];
  for (let i = start; i <= end; i++) {
    const path = paths[i];
    if (path === undefined) continue;
    const node = byPath.get(path);
    if (!isNodeCheckable(node)) continue;
    if (nodeUnmetDeps(node, byPath).length > 0) continue;
    out.push(path);
  }
  return out;
}
