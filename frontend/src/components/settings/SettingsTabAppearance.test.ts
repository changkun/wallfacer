import { describe, expect, it, beforeEach } from 'vitest';
import { createApp, nextTick } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import SettingsTabAppearance from './SettingsTabAppearance.vue';
import { PALETTES, usePrefsStore } from '../../stores/prefs';

// Appearance (specs/shared/console-redesign/settings.md): the mode is a
// segmented control and the palette roster comes from the prefs store, so a
// new preset appears without a template change.
function mount() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(SettingsTabAppearance);
  const pinia = createPinia();
  setActivePinia(pinia);
  app.use(pinia);
  app.mount(host);
  return { host, app, pinia };
}

beforeEach(() => { document.documentElement.removeAttribute('data-palette'); });

describe('SettingsTabAppearance', () => {
  it('lists every palette from the roster and marks the current one', async () => {
    const { host, app } = mount();
    const cards = host.querySelectorAll('.ap-palette');
    expect(cards.length).toBe(PALETTES.length);
    expect(Array.from(cards).map((c) => c.getAttribute('data-palette'))).toEqual(PALETTES.map((p) => p.name));
    expect(host.querySelector('.ap-palette.is-active')!.getAttribute('data-palette')).toBe('clay');
    app.unmount(); host.remove();
  });

  it('a swatch click sets the palette and the seg sets the mode', async () => {
    const { host, app, pinia } = mount();
    const prefs = usePrefsStore(pinia);
    (host.querySelector('[data-palette="paper"]') as HTMLButtonElement).click();
    await nextTick();
    expect(prefs.palette).toBe('paper');
    expect(host.querySelector('.ap-palette.is-active')!.getAttribute('data-palette')).toBe('paper');
    (host.querySelector('[data-mode="dark"]') as HTMLButtonElement).click();
    await nextTick();
    expect(prefs.theme).toBe('dark');
    expect(host.querySelector('.ap-modes .seg-btn.on')!.getAttribute('data-mode')).toBe('dark');
    prefs.setPalette('clay');
    app.unmount(); host.remove();
  });
});
