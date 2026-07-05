// HeroSim is the marketing hero's self-playing product scene. These pin the
// property the design relies on: the scene is complete static markup (so
// vite-ssg prerenders it and prefers-reduced-motion users see a meaningful
// still) and is exposed to assistive tech as a single labelled image.

import { describe, it, expect, afterEach } from 'vitest';
import { createApp, h, type App } from 'vue';
import HeroSim from './HeroSim.vue';

let app: App | null = null;

function render(): HTMLElement {
  const host = document.createElement('div');
  document.body.appendChild(host);
  app = createApp({ render: () => h(HeroSim) });
  app.mount(host);
  return host;
}

afterEach(() => {
  app?.unmount();
  app = null;
  document.body.innerHTML = '';
});

describe('HeroSim', () => {
  it('renders the full static scene (board columns, cards, pipeline nodes)', () => {
    const host = render();
    expect(host.querySelectorAll('.hs-col')).toHaveLength(3);
    expect(host.querySelectorAll('.hs-card').length).toBeGreaterThanOrEqual(3);
    const nodes = Array.from(host.querySelectorAll('.hs-node')).map((n) => n.textContent);
    expect(nodes).toEqual(['task', 'implement', 'test', 'commit', 'title', 'oversight']);
    expect(host.querySelector('.hs-edges')).not.toBeNull();
  });

  it('exposes the scene as a labelled image for assistive tech', () => {
    const host = render();
    const root = host.querySelector('.hero-sim') as HTMLElement;
    expect(root.getAttribute('role')).toBe('img');
    expect(root.getAttribute('aria-label')).toBeTruthy();
  });
});
