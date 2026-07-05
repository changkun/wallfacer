// The color-palette axis (Slack-style selectable themes) is orthogonal to
// light/dark: prefs.palette persists at wallfacer-palette and applies as
// <html data-palette>, with the default palette (clay) carrying no attribute
// so SSG output matches the default first paint. See
// specs/shared/visual-identity/theme-system.md.
import { beforeEach, describe, expect, it } from 'vitest';
import { createPinia, setActivePinia } from 'pinia';

// The environment's localStorage is not fully implemented; back it with a
// plain Map so the store's read/persist paths run for real.
const backing = new Map<string, string>();
Object.defineProperty(window, 'localStorage', {
  configurable: true,
  value: {
    getItem: (k: string) => (backing.has(k) ? backing.get(k)! : null),
    setItem: (k: string, v: string) => { backing.set(k, String(v)); },
    removeItem: (k: string) => { backing.delete(k); },
    clear: () => { backing.clear(); },
  },
});

async function freshStore() {
  // The store module applies prefs at definition time; re-import fresh so
  // each test sees the current localStorage state.
  const mod = await import('./prefs');
  setActivePinia(createPinia());
  return { store: mod.usePrefsStore(), PALETTES: mod.PALETTES };
}

beforeEach(() => {
  backing.delete('wallfacer-palette');
  document.documentElement.removeAttribute('data-palette');
});

describe('prefs palette axis', () => {
  it('defaults to clay with no data-palette attribute', async () => {
    const { store } = await freshStore();
    expect(store.palette).toBe('clay');
    expect(document.documentElement.hasAttribute('data-palette')).toBe(false);
  });

  it('setPalette applies the attribute and persists', async () => {
    const { store } = await freshStore();
    store.setPalette('amber');
    await Promise.resolve();
    expect(document.documentElement.getAttribute('data-palette')).toBe('amber');
    expect(window.localStorage.getItem('wallfacer-palette')).toBe('amber');
  });

  it('returning to clay removes the attribute', async () => {
    const { store } = await freshStore();
    store.setPalette('rose');
    await Promise.resolve();
    store.setPalette('clay');
    await Promise.resolve();
    expect(document.documentElement.hasAttribute('data-palette')).toBe(false);
  });

  it('an unknown stored value falls back to clay', async () => {
    window.localStorage.setItem('wallfacer-palette', 'chartreuse');
    const { store } = await freshStore();
    expect(store.palette).toBe('clay');
  });

  it('every roster entry has a distinct name and four swatches', async () => {
    const { PALETTES } = await freshStore();
    const names = PALETTES.map((p) => p.name);
    expect(new Set(names).size).toBe(names.length);
    expect(names[0]).toBe('clay');
    for (const p of PALETTES) expect(p.swatches).toHaveLength(4);
  });
});
