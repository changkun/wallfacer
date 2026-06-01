// Terminal panel height persistence (mirrors ui/js/status-bar.js). The panel
// grows upward from the status bar; height is clamped to [MIN, 80% of the
// viewport] and persisted under PANEL_HEIGHT_KEY across reloads.

export const PANEL_HEIGHT_KEY = 'wallfacer-panel-height';
export const PANEL_MIN_HEIGHT = 120;

export function maxPanelHeight(viewportH: number): number {
  return Math.round((viewportH || 0) * 0.8);
}

export function clampPanelHeight(value: number, viewportH: number): number {
  const max = Math.max(PANEL_MIN_HEIGHT, maxPanelHeight(viewportH));
  return Math.min(max, Math.max(PANEL_MIN_HEIGHT, Math.round(value)));
}
