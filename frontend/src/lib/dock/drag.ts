// Dockable panel workspace — drag controller contract + pure hit-testing.
//
// DockWorkspace provides a DockDragApi (under DOCK_DRAG_KEY); panels inject it
// and call begin() from a header drag handle. The pointer → drop-zone math is a
// pure function so it can be unit-tested without a DOM (see drag.test.ts).

import type { InjectionKey } from 'vue';
import type { DockRegion, PanelId } from './types';

export interface DockDragApi {
  // Start dragging `panel` from a header mousedown.
  begin: (panel: PanelId, e: MouseEvent) => void;
}

export const DOCK_DRAG_KEY: InjectionKey<DockDragApi> = Symbol('dock-drag');

export interface Rect { left: number; top: number; width: number; height: number }

// How close to an edge (as a fraction of the workspace) the pointer must be for
// that edge's drop zone to activate. Beyond this band, the pointer is in the
// dead center and no zone is selected (the drop is a no-op).
export const DROP_EDGE_BAND = 0.3;

// Resolve a pointer position within the workspace rect to the drop zone it
// would dock into, or null when the pointer is in the center dead zone. Picks
// the nearest edge; ties resolve in left/right/top/bottom order.
export function hitTestZone(rect: Rect, x: number, y: number, band = DROP_EDGE_BAND): DockRegion | null {
  if (rect.width <= 0 || rect.height <= 0) return null;
  const fx = (x - rect.left) / rect.width;
  const fy = (y - rect.top) / rect.height;
  // Outside the workspace entirely: no zone.
  if (fx < 0 || fx > 1 || fy < 0 || fy > 1) return null;
  const dist: { region: DockRegion; d: number }[] = [
    { region: 'left', d: fx },
    { region: 'right', d: 1 - fx },
    { region: 'top', d: fy },
    { region: 'bottom', d: 1 - fy },
  ];
  let best = dist[0];
  for (const c of dist) if (c.d < best.d) best = c;
  return best.d <= band ? best.region : null;
}
