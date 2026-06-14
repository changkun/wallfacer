import { defineStore } from 'pinia';
import { ref, computed, watch } from 'vue';
import { getStored, setStored } from '../lib/storage';
import { PANEL_HEIGHT_KEY } from '../lib/panelHeight';
import {
  deserialize,
  serialize,
  migrateLegacy,
  dockPanel,
  ensurePanel,
  closePanel as closePanelOp,
  resizeRegion,
  activatePanel,
  toggleMaximize as toggleMaximizeOp,
  restorePanel,
  findPanelRegion,
  isPanelPresent,
} from '../lib/dock/layout';
import {
  DockLayout,
  DockRegion,
  PanelId,
  DOCK_LAYOUT_KEY,
  DEFAULT_REGION_SIZE,
} from '../lib/dock/types';

// Default edge a panel opens into when it has no remembered region.
const DEFAULT_REGION: Record<PanelId, DockRegion> = {
  terminal: 'bottom',
  explorer: 'left',
};

function loadLayout(): DockLayout {
  const stored = deserialize(getStored(DOCK_LAYOUT_KEY));
  if (stored) return stored;
  // First run on this version: carry the legacy bottom-drawer height preference
  // forward (without auto-opening the terminal — see migrateLegacy).
  return migrateLegacy(getStored(PANEL_HEIGHT_KEY));
}

// Owns the dockable panel layout (terminal, explorer, future panels). The layout
// tree and all mutations live in lib/dock; this store adds Vue reactivity,
// localStorage persistence, last-region memory, and viewport-aware resize
// clamping. It is the source of truth for whether the terminal is open; the ui
// store delegates to it.
export const useDockStore = defineStore('dock', () => {
  const layout = ref<DockLayout>(loadLayout());

  // Remember where each panel last lived so re-opening returns it there rather
  // than always snapping back to the default edge.
  const lastRegion = ref<Record<PanelId, DockRegion>>({});
  for (const panel of ['terminal', 'explorer'] as const) {
    const r = findPanelRegion(layout.value, panel);
    if (r) lastRegion.value[panel] = r;
  }

  // Persist synchronously on every layout change. Mutating actions replace the
  // whole layout object, so this is cheap; resize drags commit a single value on
  // pointer-up (the component drives the live drag via CSS), so there is no
  // per-mousemove write thrash.
  watch(layout, (l) => setStored(DOCK_LAYOUT_KEY, serialize(l)), { deep: true, flush: 'sync' });

  const maximized = computed(() => layout.value.maximized);

  function isOpen(panel: PanelId): boolean {
    return isPanelPresent(layout.value, panel);
  }
  function regionOf(panel: PanelId): DockRegion | null {
    return findPanelRegion(layout.value, panel);
  }
  function sizeOf(region: DockRegion): number {
    return layout.value.sizes[region] ?? DEFAULT_REGION_SIZE[region];
  }

  function openPanel(panel: PanelId) {
    const region = lastRegion.value[panel] ?? DEFAULT_REGION[panel] ?? 'bottom';
    layout.value = ensurePanel(layout.value, panel, region);
    lastRegion.value[panel] = regionOf(panel) ?? region;
  }
  function closePanel(panel: PanelId) {
    const r = regionOf(panel);
    if (r) lastRegion.value[panel] = r;
    layout.value = closePanelOp(layout.value, panel);
  }
  function togglePanel(panel: PanelId) {
    if (isOpen(panel)) closePanel(panel);
    else openPanel(panel);
  }
  function dockTo(panel: PanelId, region: DockRegion) {
    layout.value = dockPanel(layout.value, panel, region);
    lastRegion.value[panel] = region;
  }
  function activate(panel: PanelId) {
    layout.value = activatePanel(layout.value, panel);
  }

  // Viewport-derived ceiling for a region drag: 80% of the relevant axis.
  function regionMax(region: DockRegion): number {
    const vw = typeof window !== 'undefined' ? window.innerWidth : 0;
    const vh = typeof window !== 'undefined' ? window.innerHeight : 0;
    const axis = region === 'left' || region === 'right' ? vw : vh;
    return Math.round(axis * 0.8);
  }
  function resize(region: DockRegion, size: number) {
    layout.value = resizeRegion(layout.value, region, size, regionMax(region));
  }

  function toggleMaximize(panel: PanelId) {
    layout.value = toggleMaximizeOp(layout.value, panel);
  }
  function restore() {
    layout.value = restorePanel(layout.value);
  }

  // Terminal conveniences (the ui store delegates these for back-compat).
  const terminalOpen = computed(() => isOpen('terminal'));
  function openTerminal() { openPanel('terminal'); }
  function closeTerminal() { closePanel('terminal'); }
  function toggleTerminal() { togglePanel('terminal'); }

  return {
    layout, maximized,
    isOpen, regionOf, sizeOf, regionMax,
    openPanel, closePanel, togglePanel, dockTo, activate, resize,
    toggleMaximize, restore,
    terminalOpen, openTerminal, closeTerminal, toggleTerminal,
  };
});
