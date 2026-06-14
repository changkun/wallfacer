// Dockable panel workspace — pure layout reducers.
//
// Every function here is a pure `(layout, ...) => layout` (or a pure query) over
// the DockLayout tree. No Vue, no DOM, no localStorage. The dock store
// (stores/dock.ts) wraps these and handles reactivity + persistence; these are
// the unit-tested core (see layout.test.ts).

import {
  DockLayout,
  DockNode,
  DockRegion,
  PanelId,
  DOCK_LAYOUT_VERSION,
  DEFAULT_REGION_SIZE,
  REGION_MIN_SIZE,
  DOCK_REGIONS,
} from './types';

// ---------------------------------------------------------------------------
// Construction
// ---------------------------------------------------------------------------

// The empty layout: no docked panels. Matches the current default where the
// terminal is closed until the user opens it.
export function defaultLayout(): DockLayout {
  return { regions: {}, sizes: {}, maximized: null, version: DOCK_LAYOUT_VERSION };
}

function clone(layout: DockLayout): DockLayout {
  return {
    regions: { ...layout.regions },
    sizes: { ...layout.sizes },
    maximized: layout.maximized,
    version: layout.version,
  };
}

// Allocate a group id unique within the layout: scans existing `g<n>` ids and
// returns the next integer suffix. Pure and deterministic (no Math.random), so
// tests get stable ids.
function nextGroupId(layout: DockLayout): string {
  let max = 0;
  for (const region of DOCK_REGIONS) {
    const node = layout.regions[region];
    if (node) {
      eachGroup(node, (g) => {
        const m = /^g(\d+)$/.exec(g.id);
        if (m) max = Math.max(max, Number(m[1]));
      });
    }
  }
  return `g${max + 1}`;
}

function group(id: string, panel: PanelId): Extract<DockNode, { kind: 'group' }> {
  return { kind: 'group', id, tabs: [panel], active: panel };
}

// ---------------------------------------------------------------------------
// Tree walking / queries
// ---------------------------------------------------------------------------

// Visit every group node in a subtree (depth-first).
export function eachGroup(
  node: DockNode,
  cb: (g: Extract<DockNode, { kind: 'group' }>) => void,
): void {
  if (node.kind === 'group') { cb(node); return; }
  for (const child of node.children) eachGroup(child, cb);
}

// The first group in a subtree (depth-first), or null if none.
function firstGroup(node: DockNode): Extract<DockNode, { kind: 'group' }> | null {
  if (node.kind === 'group') return node;
  for (const child of node.children) {
    const g = firstGroup(child);
    if (g) return g;
  }
  return null;
}

function nodeContains(node: DockNode, panel: PanelId): boolean {
  let found = false;
  eachGroup(node, (g) => { if (g.tabs.includes(panel)) found = true; });
  return found;
}

// Which region currently holds `panel`, or null if it is not docked anywhere.
export function findPanelRegion(layout: DockLayout, panel: PanelId): DockRegion | null {
  for (const region of DOCK_REGIONS) {
    const node = layout.regions[region];
    if (node && nodeContains(node, panel)) return region;
  }
  return null;
}

export function isPanelPresent(layout: DockLayout, panel: PanelId): boolean {
  return findPanelRegion(layout, panel) !== null;
}

// Every panel id present in the layout, in region/DFS order.
export function panelIds(layout: DockLayout): PanelId[] {
  const ids: PanelId[] = [];
  for (const region of DOCK_REGIONS) {
    const node = layout.regions[region];
    if (node) eachGroup(node, (g) => ids.push(...g.tabs));
  }
  return ids;
}

// ---------------------------------------------------------------------------
// Removal / collapse
// ---------------------------------------------------------------------------

// Remove a panel from a subtree, collapsing empty groups and degenerate splits.
// Returns the rewritten node, or null if the whole subtree became empty.
function removeFromNode(node: DockNode, panel: PanelId): DockNode | null {
  if (node.kind === 'group') {
    if (!node.tabs.includes(panel)) return node;
    const tabs = node.tabs.filter((t) => t !== panel);
    if (tabs.length === 0) return null;
    const active = node.active === panel ? tabs[0] : node.active;
    return { kind: 'group', id: node.id, tabs, active };
  }
  const kept: DockNode[] = [];
  const sizes: number[] = [];
  node.children.forEach((child, i) => {
    const next = removeFromNode(child, panel);
    if (next) { kept.push(next); sizes.push(node.sizes[i] ?? 1); }
  });
  if (kept.length === 0) return null;
  if (kept.length === 1) return kept[0]; // collapse a single-child split
  return { kind: 'split', dir: node.dir, sizes: normalize(sizes), children: kept };
}

// Renormalize split sizes to sum to 1, guarding against an all-zero input.
function normalize(sizes: number[]): number[] {
  const total = sizes.reduce((a, b) => a + b, 0);
  if (total <= 0) return sizes.map(() => 1 / sizes.length);
  return sizes.map((s) => s / total);
}

// Remove a panel from wherever it lives, pruning a region that becomes empty
// (and its stored size). Returns a fresh layout.
export function closePanel(layout: DockLayout, panel: PanelId): DockLayout {
  const next = clone(layout);
  for (const region of DOCK_REGIONS) {
    const node = next.regions[region];
    if (!node || !nodeContains(node, panel)) continue;
    const rewritten = removeFromNode(node, panel);
    if (rewritten) {
      next.regions[region] = rewritten;
    } else {
      delete next.regions[region];
      delete next.sizes[region];
    }
  }
  if (next.maximized === panel) next.maximized = null;
  return next;
}

// ---------------------------------------------------------------------------
// Docking
// ---------------------------------------------------------------------------

// Move `panel` to `region`, merging it as a new active tab in that region's
// primary (first) group, or seeding the region if empty. Removes the panel from
// its previous location first. Returns a fresh layout.
export function dockPanel(layout: DockLayout, panel: PanelId, region: DockRegion): DockLayout {
  const wasMax = layout.maximized === panel;
  const next = closePanel(layout, panel);
  const target = next.regions[region];
  if (!target) {
    next.regions[region] = group(nextGroupId(next), panel);
    next.sizes[region] = next.sizes[region] ?? DEFAULT_REGION_SIZE[region];
  } else {
    const g = firstGroup(target);
    if (g) {
      g.tabs = [...g.tabs, panel];
      g.active = panel;
    } else {
      next.regions[region] = group(nextGroupId(next), panel);
    }
    if (next.sizes[region] == null) next.sizes[region] = DEFAULT_REGION_SIZE[region];
  }
  if (wasMax) next.maximized = panel;
  return next;
}

// Ensure `panel` is docked somewhere; if absent, dock it to `region`. No-op when
// already present (used by "open terminal" so it returns to its last spot).
export function ensurePanel(layout: DockLayout, panel: PanelId, region: DockRegion): DockLayout {
  if (isPanelPresent(layout, panel)) return layout;
  return dockPanel(layout, panel, region);
}

// ---------------------------------------------------------------------------
// Resize
// ---------------------------------------------------------------------------

// Clamp a region pixel size to [REGION_MIN_SIZE, max]. `max` (optional) is the
// viewport-derived ceiling supplied by the component; omitted in pure tests.
export function clampRegionSize(value: number, max?: number): number {
  const lo = REGION_MIN_SIZE;
  const hi = max != null ? Math.max(lo, max) : Infinity;
  return Math.min(hi, Math.max(lo, Math.round(value)));
}

export function resizeRegion(
  layout: DockLayout,
  region: DockRegion,
  size: number,
  max?: number,
): DockLayout {
  if (!layout.regions[region]) return layout;
  const next = clone(layout);
  next.sizes[region] = clampRegionSize(size, max);
  return next;
}

// ---------------------------------------------------------------------------
// Tab activation / maximize
// ---------------------------------------------------------------------------

// Make `panel` the visible tab in its group. Returns a fresh layout (or the same
// if the panel is absent or already active).
export function activatePanel(layout: DockLayout, panel: PanelId): DockLayout {
  const region = findPanelRegion(layout, panel);
  if (!region) return layout;
  const node = layout.regions[region]!;
  let changed = false;
  const rewrite = (n: DockNode): DockNode => {
    if (n.kind === 'group') {
      if (n.tabs.includes(panel) && n.active !== panel) {
        changed = true;
        return { ...n, active: panel };
      }
      return n;
    }
    return { ...n, children: n.children.map(rewrite) };
  };
  const rewritten = rewrite(node);
  if (!changed) return layout;
  const next = clone(layout);
  next.regions[region] = rewritten;
  return next;
}

export function maximizePanel(layout: DockLayout, panel: PanelId): DockLayout {
  if (!isPanelPresent(layout, panel) || layout.maximized === panel) return layout;
  const next = clone(layout);
  next.maximized = panel;
  return next;
}

export function restorePanel(layout: DockLayout): DockLayout {
  if (layout.maximized == null) return layout;
  const next = clone(layout);
  next.maximized = null;
  return next;
}

// Toggle maximize for `panel`: maximize if it isn't the maximized one, else
// restore. Convenience for the maximize button.
export function toggleMaximize(layout: DockLayout, panel: PanelId): DockLayout {
  return layout.maximized === panel ? restorePanel(layout) : maximizePanel(layout, panel);
}

// ---------------------------------------------------------------------------
// Serialization / migration
// ---------------------------------------------------------------------------

export function serialize(layout: DockLayout): string {
  return JSON.stringify(layout);
}

function isValidNode(value: unknown): value is DockNode {
  if (!value || typeof value !== 'object') return false;
  const n = value as Record<string, unknown>;
  if (n.kind === 'group') {
    return Array.isArray(n.tabs) && n.tabs.length > 0
      && typeof n.id === 'string'
      && typeof n.active === 'string'
      && (n.tabs as unknown[]).includes(n.active)
      && (n.tabs as unknown[]).every((t) => typeof t === 'string');
  }
  if (n.kind === 'split') {
    return (n.dir === 'row' || n.dir === 'col')
      && Array.isArray(n.children) && n.children.length >= 2
      && Array.isArray(n.sizes) && n.sizes.length === n.children.length
      && n.children.every(isValidNode);
  }
  return false;
}

// Parse a persisted layout, returning null if it is missing, malformed, or from
// an unknown version. The store falls back to defaultLayout() on null.
export function deserialize(raw: string | null): DockLayout | null {
  if (!raw) return null;
  let parsed: unknown;
  try { parsed = JSON.parse(raw); } catch { return null; }
  if (!parsed || typeof parsed !== 'object') return null;
  const p = parsed as Record<string, unknown>;
  if (p.version !== DOCK_LAYOUT_VERSION) return null;
  if (!p.regions || typeof p.regions !== 'object') return null;
  const regions: Partial<Record<DockRegion, DockNode>> = {};
  for (const region of DOCK_REGIONS) {
    const node = (p.regions as Record<string, unknown>)[region];
    if (node === undefined) continue;
    if (!isValidNode(node)) return null;
    regions[region] = node;
  }
  const sizes: Partial<Record<DockRegion, number>> = {};
  const rawSizes = (p.sizes && typeof p.sizes === 'object') ? p.sizes as Record<string, unknown> : {};
  for (const region of DOCK_REGIONS) {
    const s = rawSizes[region];
    if (typeof s === 'number' && Number.isFinite(s)) sizes[region] = clampRegionSize(s);
  }
  const maximized = typeof p.maximized === 'string' ? p.maximized : null;
  return { regions, sizes, maximized, version: DOCK_LAYOUT_VERSION };
}

// Seed a layout from the legacy single-bottom-drawer state: a bottom-docked
// terminal at the persisted 'wallfacer-panel-height' (or the default). Called
// once, only when no v1 layout exists yet.
export function migrateLegacy(legacyHeight: string | null): DockLayout {
  const layout = dockPanel(defaultLayout(), 'terminal', 'bottom');
  const h = Number(legacyHeight);
  if (Number.isFinite(h) && h >= REGION_MIN_SIZE) {
    layout.sizes.bottom = clampRegionSize(h);
  }
  return layout;
}
