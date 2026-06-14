import { describe, it, expect } from 'vitest';
import {
  defaultLayout,
  dockPanel,
  ensurePanel,
  closePanel,
  findPanelRegion,
  isPanelPresent,
  panelIds,
  resizeRegion,
  clampRegionSize,
  activatePanel,
  maximizePanel,
  restorePanel,
  toggleMaximize,
  serialize,
  deserialize,
  migrateLegacy,
} from './layout';
import { DockLayout, DockNode, DEFAULT_REGION_SIZE, REGION_MIN_SIZE, DOCK_LAYOUT_VERSION } from './types';

function groupNode(region: DockLayout['regions'][keyof DockLayout['regions']]): Extract<DockNode, { kind: 'group' }> {
  if (!region || region.kind !== 'group') throw new Error('expected a group node');
  return region;
}

describe('defaultLayout', () => {
  it('is empty with no docked panels', () => {
    const l = defaultLayout();
    expect(l.regions).toEqual({});
    expect(l.maximized).toBeNull();
    expect(panelIds(l)).toEqual([]);
  });
});

describe('dockPanel', () => {
  it('seeds an empty region with a single-tab group and a default size', () => {
    const l = dockPanel(defaultLayout(), 'terminal', 'bottom');
    expect(findPanelRegion(l, 'terminal')).toBe('bottom');
    expect(groupNode(l.regions.bottom).tabs).toEqual(['terminal']);
    expect(l.sizes.bottom).toBe(DEFAULT_REGION_SIZE.bottom);
  });

  it('moves a panel from one region to another without duplicating it', () => {
    let l = dockPanel(defaultLayout(), 'terminal', 'bottom');
    l = dockPanel(l, 'terminal', 'right');
    expect(findPanelRegion(l, 'terminal')).toBe('right');
    expect(l.regions.bottom).toBeUndefined();
    expect(panelIds(l)).toEqual(['terminal']);
  });

  it('merges into the primary group as the active tab when the region is occupied', () => {
    let l = dockPanel(defaultLayout(), 'terminal', 'left');
    l = dockPanel(l, 'explorer', 'left');
    const g = groupNode(l.regions.left);
    expect(g.tabs).toEqual(['terminal', 'explorer']);
    expect(g.active).toBe('explorer');
  });

  it('assigns unique group ids across regions', () => {
    let l = dockPanel(defaultLayout(), 'terminal', 'bottom');
    l = dockPanel(l, 'explorer', 'right');
    expect(groupNode(l.regions.bottom).id).not.toBe(groupNode(l.regions.right).id);
  });

  it('preserves a region size that already exists when merging', () => {
    let l = dockPanel(defaultLayout(), 'terminal', 'bottom');
    l = resizeRegion(l, 'bottom', 400);
    l = dockPanel(l, 'explorer', 'bottom');
    expect(l.sizes.bottom).toBe(400);
  });

  it('does not mutate the input layout', () => {
    const l = defaultLayout();
    dockPanel(l, 'terminal', 'bottom');
    expect(l.regions).toEqual({});
  });
});

describe('ensurePanel', () => {
  it('docks the panel when absent', () => {
    const l = ensurePanel(defaultLayout(), 'terminal', 'bottom');
    expect(isPanelPresent(l, 'terminal')).toBe(true);
  });

  it('is a no-op when already present, keeping its current region', () => {
    const seeded = dockPanel(defaultLayout(), 'terminal', 'right');
    const l = ensurePanel(seeded, 'terminal', 'bottom');
    expect(l).toBe(seeded);
    expect(findPanelRegion(l, 'terminal')).toBe('right');
  });
});

describe('closePanel', () => {
  it('removes a panel and prunes the now-empty region and its size', () => {
    let l = dockPanel(defaultLayout(), 'terminal', 'bottom');
    l = closePanel(l, 'terminal');
    expect(isPanelPresent(l, 'terminal')).toBe(false);
    expect(l.regions.bottom).toBeUndefined();
    expect(l.sizes.bottom).toBeUndefined();
  });

  it('keeps the region and reassigns active when closing one of several tabs', () => {
    let l = dockPanel(defaultLayout(), 'terminal', 'left');
    l = dockPanel(l, 'explorer', 'left'); // active = explorer
    l = closePanel(l, 'explorer');
    const g = groupNode(l.regions.left);
    expect(g.tabs).toEqual(['terminal']);
    expect(g.active).toBe('terminal');
  });

  it('clears maximize when the maximized panel is closed', () => {
    let l = dockPanel(defaultLayout(), 'terminal', 'bottom');
    l = maximizePanel(l, 'terminal');
    l = closePanel(l, 'terminal');
    expect(l.maximized).toBeNull();
  });
});

describe('resize', () => {
  it('clamps below the minimum', () => {
    expect(clampRegionSize(10)).toBe(REGION_MIN_SIZE);
  });
  it('clamps above the provided max', () => {
    expect(clampRegionSize(900, 800)).toBe(800);
  });
  it('rounds an in-range value', () => {
    expect(clampRegionSize(260.6)).toBe(261);
  });
  it('resizeRegion stores the clamped size only for an existing region', () => {
    let l = dockPanel(defaultLayout(), 'terminal', 'bottom');
    l = resizeRegion(l, 'bottom', 50);
    expect(l.sizes.bottom).toBe(REGION_MIN_SIZE);
    const before = resizeRegion(l, 'top', 300);
    expect(before.sizes.top).toBeUndefined(); // top region does not exist
  });
});

describe('activatePanel', () => {
  it('switches the active tab', () => {
    let l = dockPanel(defaultLayout(), 'terminal', 'left');
    l = dockPanel(l, 'explorer', 'left');
    l = activatePanel(l, 'terminal');
    expect(groupNode(l.regions.left).active).toBe('terminal');
  });
  it('returns the same reference when already active or absent', () => {
    const l = dockPanel(defaultLayout(), 'terminal', 'left');
    expect(activatePanel(l, 'terminal')).toBe(l);
    expect(activatePanel(l, 'ghost')).toBe(l);
  });
});

describe('maximize', () => {
  it('maximizes and restores a present panel', () => {
    let l = dockPanel(defaultLayout(), 'terminal', 'bottom');
    l = maximizePanel(l, 'terminal');
    expect(l.maximized).toBe('terminal');
    l = restorePanel(l);
    expect(l.maximized).toBeNull();
  });
  it('refuses to maximize an absent panel', () => {
    const l = defaultLayout();
    expect(maximizePanel(l, 'terminal')).toBe(l);
  });
  it('toggles', () => {
    let l = dockPanel(defaultLayout(), 'terminal', 'bottom');
    l = toggleMaximize(l, 'terminal');
    expect(l.maximized).toBe('terminal');
    l = toggleMaximize(l, 'terminal');
    expect(l.maximized).toBeNull();
  });
  it('keeps a panel maximized across a dock move', () => {
    let l = dockPanel(defaultLayout(), 'terminal', 'bottom');
    l = maximizePanel(l, 'terminal');
    l = dockPanel(l, 'terminal', 'right');
    expect(l.maximized).toBe('terminal');
    expect(findPanelRegion(l, 'terminal')).toBe('right');
  });
});

describe('serialize / deserialize', () => {
  it('round-trips a non-trivial layout', () => {
    let l = dockPanel(defaultLayout(), 'terminal', 'left');
    l = dockPanel(l, 'explorer', 'bottom');
    l = maximizePanel(l, 'explorer');
    const back = deserialize(serialize(l));
    expect(back).toEqual(l);
  });
  it('returns null on garbage, empty, or wrong version', () => {
    expect(deserialize(null)).toBeNull();
    expect(deserialize('not json')).toBeNull();
    expect(deserialize(JSON.stringify({ regions: {}, sizes: {}, maximized: null, version: 999 }))).toBeNull();
  });
  it('rejects a structurally invalid node', () => {
    const bad = { regions: { bottom: { kind: 'group', id: 'g1', tabs: [], active: 'x' } }, sizes: {}, maximized: null, version: DOCK_LAYOUT_VERSION };
    expect(deserialize(JSON.stringify(bad))).toBeNull();
  });
  it('clamps an out-of-range stored size on load', () => {
    const raw = { regions: { bottom: { kind: 'group', id: 'g1', tabs: ['terminal'], active: 'terminal' } }, sizes: { bottom: 5 }, maximized: null, version: DOCK_LAYOUT_VERSION };
    const back = deserialize(JSON.stringify(raw));
    expect(back?.sizes.bottom).toBe(REGION_MIN_SIZE);
  });
});

describe('migrateLegacy', () => {
  it('does not auto-dock the terminal (legacy default was closed)', () => {
    const l = migrateLegacy('340');
    expect(isPanelPresent(l, 'terminal')).toBe(false);
  });
  it('carries the legacy height forward as the bottom size preference', () => {
    const l = migrateLegacy('340');
    expect(l.sizes.bottom).toBe(340);
    // ...and the terminal adopts it the first time it docks to the bottom.
    const docked = dockPanel(l, 'terminal', 'bottom');
    expect(docked.sizes.bottom).toBe(340);
  });
  it('leaves no bottom size when no legacy height is stored', () => {
    const l = migrateLegacy(null);
    expect(l.sizes.bottom).toBeUndefined();
    expect(dockPanel(l, 'terminal', 'bottom').sizes.bottom).toBe(DEFAULT_REGION_SIZE.bottom);
  });
  it('ignores a sub-minimum legacy height', () => {
    const l = migrateLegacy('10');
    expect(l.sizes.bottom).toBeUndefined();
  });
});
