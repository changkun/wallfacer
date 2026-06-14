// Dockable panel workspace — layout data model.
//
// The layout is a serializable tree manipulated only through the pure reducers
// in ./layout.ts. Vue components read this tree and dispatch reducer calls; they
// never mutate it directly. Keeping the model free of DOM/Vue concerns is what
// makes dock/split/move/resize/migrate unit-testable without a browser (see the
// editor-center model in specs/local/dockable-panel-workspace.md).

// The four edges a panel can dock to. The editor (RouterView page content) is
// the fixed center and is never part of the tree.
export type DockRegion = 'left' | 'right' | 'top' | 'bottom';

export const DOCK_REGIONS: readonly DockRegion[] = ['left', 'right', 'top', 'bottom'];

// Identifies a panel body. 'terminal' and 'explorer' ship first; the type stays
// open so future panels (inline file panel, etc.) slot in without a model change.
export type PanelId = string;

// A node in a region's split-tree. A `group` is a tab-group holding one or more
// panels (only one shows at a time); a `split` divides space among children in a
// fixed direction with proportional `sizes` (one entry per child, summing ~1).
export type DockNode =
  | { kind: 'split'; dir: 'row' | 'col'; sizes: number[]; children: DockNode[] }
  | { kind: 'group'; id: string; tabs: PanelId[]; active: PanelId };

// The full persisted layout: one optional split-tree per edge (absent = that
// edge is empty), a pixel size per edge, an optional maximized panel that
// eclipses everything, and a version for migration.
export interface DockLayout {
  regions: Partial<Record<DockRegion, DockNode>>;
  sizes: Partial<Record<DockRegion, number>>;
  maximized: PanelId | null;
  version: number;
}

export const DOCK_LAYOUT_VERSION = 1;

// localStorage key for the serialized layout. Supersedes the legacy
// 'wallfacer-panel-height' (read once by migrateLegacy in ./layout.ts).
export const DOCK_LAYOUT_KEY = 'wallfacer-dock-layout-v1';

// Default pixel size for a freshly-docked region, by orientation. Left/right are
// widths; top/bottom are heights.
export const DEFAULT_REGION_SIZE: Record<DockRegion, number> = {
  left: 320,
  right: 320,
  top: 240,
  bottom: 260,
};

// Region sizes are clamped to this floor so a panel can never be dragged shut.
export const REGION_MIN_SIZE = 120;
