import { describe, it, expect } from 'vitest';
import { chartPalette, watchPalette } from './chartPalette';

describe('chartPalette', () => {
  it('reads the ramp and surfaces from the root computed style', () => {
    const root = document.documentElement;
    root.style.setProperty('--accent', '#c45a33');
    root.style.setProperty('--ok', '#2f8a5b');
    root.style.setProperty('--bg-sunk', '#f1f0ec');
    const p = chartPalette();
    expect(p.accent).toBe('#c45a33');
    expect(p.ok).toBe('#2f8a5b');
    expect(p.bgSunk).toBe('#f1f0ec');
  });

  it('notifies on a theme change and stops after dispose', async () => {
    let calls = 0;
    const stop = watchPalette(() => { calls += 1; });
    document.documentElement.setAttribute('data-theme', 'dark');
    await new Promise((r) => setTimeout(r, 0));
    expect(calls).toBe(1);
    stop();
    document.documentElement.setAttribute('data-theme', 'light');
    await new Promise((r) => setTimeout(r, 0));
    expect(calls).toBe(1);
  });
});
