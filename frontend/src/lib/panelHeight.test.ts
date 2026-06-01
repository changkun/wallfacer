import { describe, it, expect } from 'vitest';
import { clampPanelHeight, maxPanelHeight, PANEL_MIN_HEIGHT } from './panelHeight';

describe('clampPanelHeight', () => {
  it('floors at the minimum height', () => {
    expect(clampPanelHeight(50, 1000)).toBe(PANEL_MIN_HEIGHT);
  });
  it('caps at 80% of the viewport', () => {
    expect(clampPanelHeight(900, 1000)).toBe(800);
    expect(maxPanelHeight(1000)).toBe(800);
  });
  it('passes through and rounds an in-range value', () => {
    expect(clampPanelHeight(260.6, 1000)).toBe(261);
  });
  it('keeps the minimum usable when the viewport is tiny', () => {
    expect(clampPanelHeight(200, 100)).toBe(PANEL_MIN_HEIGHT);
  });
});
