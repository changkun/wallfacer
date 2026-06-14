<script setup lang="ts">
// Dockable panel workspace shell. Wraps the RouterView page (the fixed center
// "editor") and lays dockable panels around the four edges, each in a
// resizable region. A maximized panel eclipses everything via an absolute
// overlay. The live panel (terminal) is mounted once here and teleported into
// its current region's mount point so its xterm + WebSocket survive moves
// (see specs/local/dockable-panel-workspace.md).
import { ref, computed, reactive, provide } from 'vue';
import TerminalPanel from './TerminalPanel.vue';
import { useDockStore } from '../stores/dock';
import { clampRegionSize } from '../lib/dock/layout';
import { DOCK_DRAG_KEY, hitTestZone } from '../lib/dock/drag';
import type { DockRegion, PanelId } from '../lib/dock/types';

const dock = useDockStore();
const wsEl = ref<HTMLElement | null>(null);

// Mount points the panels teleport into. Each edge has one; `maxEl` hosts a
// maximized panel. v-show (not v-if) keeps maxEl in the DOM so its ref is stable.
const topEl = ref<HTMLElement | null>(null);
const leftEl = ref<HTMLElement | null>(null);
const rightEl = ref<HTMLElement | null>(null);
const bottomEl = ref<HTMLElement | null>(null);
const maxEl = ref<HTMLElement | null>(null);
const regionEl: Record<DockRegion, typeof topEl> = {
  top: topEl, left: leftEl, right: rightEl, bottom: bottomEl,
};

function occupied(region: DockRegion): boolean {
  return dock.layout.regions[region] != null;
}
const maximized = computed(() => dock.maximized != null);

// Live drag sizes: while dragging a gutter the size is driven locally (CSS only,
// no store writes); the final value commits to the store on pointer-up.
const live = reactive<Record<DockRegion, number | null>>({
  top: null, left: null, right: null, bottom: null,
});
function effSize(region: DockRegion): number {
  return live[region] ?? dock.sizeOf(region);
}
function regionStyle(region: DockRegion) {
  const px = effSize(region) + 'px';
  return region === 'left' || region === 'right' ? { width: px } : { height: px };
}

// Which mount point the terminal should teleport into right now.
const terminalTarget = computed<HTMLElement | null>(() => {
  if (dock.maximized === 'terminal') return maxEl.value;
  const r = dock.regionOf('terminal');
  return r ? regionEl[r].value : null;
});

// --- Drag-to-dock ----------------------------------------------------------
// A panel header starts a drag; an edge drop-zone overlay shows where it will
// land. Dropping on a zone docks the panel there; dropping in the center is a
// no-op. The pointer -> zone math lives in lib/dock/drag (unit-tested).
const drag = reactive<{ panel: PanelId | null; zone: DockRegion | null }>({ panel: null, zone: null });

function beginDrag(panel: PanelId, e: MouseEvent) {
  e.preventDefault();
  drag.panel = panel;
  drag.zone = null;
  function move(ev: MouseEvent) {
    const el = wsEl.value;
    if (!el) return;
    drag.zone = hitTestZone(el.getBoundingClientRect(), ev.clientX, ev.clientY);
  }
  function up() {
    document.removeEventListener('mousemove', move);
    document.removeEventListener('mouseup', up);
    const target = drag.zone;
    const p = drag.panel;
    drag.panel = null;
    drag.zone = null;
    if (p && target) {
      if (dock.maximized === p) dock.restore();
      dock.dockTo(p, target);
    }
  }
  document.addEventListener('mousemove', move);
  document.addEventListener('mouseup', up);
}

provide(DOCK_DRAG_KEY, { begin: beginDrag });

function startResize(region: DockRegion, e: MouseEvent) {
  e.preventDefault();
  const horizontal = region === 'left' || region === 'right';
  const start = horizontal ? e.clientX : e.clientY;
  const startSize = dock.sizeOf(region);
  // The gutter sits between the region and the editor; the drag direction that
  // grows the region depends on which edge it is docked to.
  const sign = region === 'right' || region === 'bottom' ? -1 : 1;
  const max = dock.regionMax(region);
  function move(ev: MouseEvent) {
    const cur = horizontal ? ev.clientX : ev.clientY;
    live[region] = clampRegionSize(startSize + (cur - start) * sign, max);
  }
  function up() {
    document.removeEventListener('mousemove', move);
    document.removeEventListener('mouseup', up);
    if (live[region] != null) dock.resize(region, live[region]!);
    live[region] = null;
  }
  document.addEventListener('mousemove', move);
  document.addEventListener('mouseup', up);
}
</script>

<template>
  <div ref="wsEl" class="dock-ws" :class="{ 'dock-ws--dragging': drag.panel }">
    <template v-if="occupied('top')">
      <div class="dock-region dock-region--top" :style="regionStyle('top')">
        <div ref="topEl" class="dock-region__mount" />
      </div>
      <div
        class="dock-gutter dock-gutter--h"
        role="separator" aria-orientation="horizontal" aria-label="Resize top panel"
        @mousedown="startResize('top', $event)"
      />
    </template>

    <div class="dock-mid">
      <template v-if="occupied('left')">
        <div class="dock-region dock-region--left" :style="regionStyle('left')">
          <div ref="leftEl" class="dock-region__mount" />
        </div>
        <div
          class="dock-gutter dock-gutter--v"
          role="separator" aria-orientation="vertical" aria-label="Resize left panel"
          @mousedown="startResize('left', $event)"
        />
      </template>

      <div class="dock-editor"><slot /></div>

      <template v-if="occupied('right')">
        <div
          class="dock-gutter dock-gutter--v"
          role="separator" aria-orientation="vertical" aria-label="Resize right panel"
          @mousedown="startResize('right', $event)"
        />
        <div class="dock-region dock-region--right" :style="regionStyle('right')">
          <div ref="rightEl" class="dock-region__mount" />
        </div>
      </template>
    </div>

    <template v-if="occupied('bottom')">
      <div
        class="dock-gutter dock-gutter--h"
        role="separator" aria-orientation="horizontal" aria-label="Resize bottom panel"
        @mousedown="startResize('bottom', $event)"
      />
      <div class="dock-region dock-region--bottom" :style="regionStyle('bottom')">
        <div ref="bottomEl" class="dock-region__mount" />
      </div>
    </template>

    <!-- Drop-zone overlay shown while dragging a panel header. -->
    <div v-if="drag.panel" class="dock-drop">
      <div
        v-for="z in (['left', 'right', 'top', 'bottom'] as const)"
        :key="z"
        class="dock-drop__zone"
        :class="[`dock-drop__zone--${z}`, { 'dock-drop__zone--active': drag.zone === z }]"
      />
    </div>

    <!-- Maximized-panel overlay. Kept mounted (v-show) so its ref is stable. -->
    <div v-show="maximized" ref="maxEl" class="dock-max" />

    <!-- Persistent panel host: mounted once, teleports into its current slot. -->
    <TerminalPanel :target="terminalTarget" />
  </div>
</template>
