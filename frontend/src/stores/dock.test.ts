import { describe, it, expect, beforeEach, vi } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';

// happy-dom ships only a partial localStorage stub, so install an in-memory one
// the store's persistence can rely on (mirrors PlanPage.resize.test.ts).
const memStore = new Map<string, string>();
vi.stubGlobal('localStorage', {
  getItem: (k: string) => (memStore.has(k) ? memStore.get(k)! : null),
  setItem: (k: string, v: string) => { memStore.set(k, String(v)); },
  removeItem: (k: string) => { memStore.delete(k); },
  clear: () => { memStore.clear(); },
});

import { useDockStore } from './dock';
import { useUiStore } from './ui';
import { getStored } from '../lib/storage';
import { deserialize } from '../lib/dock/layout';
import { DOCK_LAYOUT_KEY } from '../lib/dock/types';

describe('dock store', () => {
  beforeEach(() => {
    memStore.clear();
    setActivePinia(createPinia());
  });

  it('opens, docks, and closes the terminal', () => {
    const d = useDockStore();
    expect(d.isOpen('terminal')).toBe(false);
    d.openTerminal();
    expect(d.isOpen('terminal')).toBe(true);
    expect(d.regionOf('terminal')).toBe('bottom');
    d.dockTo('terminal', 'right');
    expect(d.regionOf('terminal')).toBe('right');
    d.closeTerminal();
    expect(d.isOpen('terminal')).toBe(false);
  });

  it('re-opens a panel into its last region', () => {
    const d = useDockStore();
    d.openTerminal();
    d.dockTo('terminal', 'left');
    d.closeTerminal();
    d.openTerminal();
    expect(d.regionOf('terminal')).toBe('left');
  });

  it('persists the layout to localStorage on change', () => {
    const d = useDockStore();
    d.openTerminal();
    const stored = deserialize(getStored(DOCK_LAYOUT_KEY));
    expect(stored && stored.regions.bottom).toBeTruthy();
  });

  it('clamps a resize to the region minimum', () => {
    const d = useDockStore();
    d.openTerminal();
    d.resize('bottom', 10);
    expect(d.sizeOf('bottom')).toBe(120);
  });

  it('toggles and clears maximize', () => {
    const d = useDockStore();
    d.openTerminal();
    d.toggleMaximize('terminal');
    expect(d.maximized).toBe('terminal');
    d.restore();
    expect(d.maximized).toBeNull();
  });

  it('the ui store reflects terminal open state through delegation', () => {
    const d = useDockStore();
    const ui = useUiStore();
    expect(ui.showTerminal).toBe(false);
    ui.toggleTerminal();
    expect(d.isOpen('terminal')).toBe(true);
    expect(ui.showTerminal).toBe(true);
    ui.closeTerminal();
    expect(ui.showTerminal).toBe(false);
  });
});
