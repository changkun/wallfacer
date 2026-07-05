// AutonomySpectrum is the landing page's interactive autonomy demo. These pin
// its contract: four stops, a complete default scene for SSG (Plan active),
// click-to-activate, and keyboard slider semantics with clamping.

import { describe, it, expect, afterEach } from 'vitest';
import { createApp, h, nextTick, type App } from 'vue';
import { createPinia } from 'pinia';
import AutonomySpectrum from './AutonomySpectrum.vue';

let app: App | null = null;

function render(): HTMLElement {
  const host = document.createElement('div');
  document.body.appendChild(host);
  app = createApp({ render: () => h(AutonomySpectrum) });
  app.use(createPinia());
  app.mount(host);
  return host;
}

afterEach(() => {
  app?.unmount();
  app = null;
  document.body.innerHTML = '';
});

function slider(host: HTMLElement): HTMLElement {
  return host.querySelector('[role="slider"]') as HTMLElement;
}

describe('AutonomySpectrum', () => {
  it('renders four stops with Plan active by default (complete SSG scene)', () => {
    const host = render();
    const stops = host.querySelectorAll('.spectrum-stop');
    expect(stops).toHaveLength(4);
    expect(stops[1].classList.contains('spectrum-stop--active')).toBe(true);
    expect(host.querySelector('.spectrum-desc')?.textContent).toContain('specs');
  });

  it('activates a stop on click and updates the slider state', async () => {
    const host = render();
    (host.querySelectorAll('.spectrum-stop')[3] as HTMLElement).click();
    await nextTick();
    expect(slider(host).getAttribute('aria-valuenow')).toBe('4');
    expect(host.querySelectorAll('.spectrum-stop')[3].classList.contains('spectrum-stop--active')).toBe(true);
  });

  it('supports arrow-key navigation with clamping', async () => {
    const host = render();
    const track = slider(host);
    track.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }));
    await nextTick();
    expect(track.getAttribute('aria-valuenow')).toBe('1');
    track.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }));
    await nextTick();
    expect(track.getAttribute('aria-valuenow')).toBe('1');
    track.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    await nextTick();
    expect(track.getAttribute('aria-valuenow')).toBe('2');
  });
});
