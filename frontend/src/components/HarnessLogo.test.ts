// HarnessLogo renders the official mark for each harness. These pin the
// behavior callers depend on: the `color` prop only tints harnesses that have
// a brand color (Claude), everything else stays currentColor so in-app badges
// adapt to theme; unknown ids fall back to a neutral glyph; and the OpenAI
// blossom's <use> refs get a per-instance id so multiple codex marks on one
// page never collide.

import { describe, it, expect, afterEach } from 'vitest';
import { createApp, h, type App } from 'vue';
import HarnessLogo from './HarnessLogo.vue';

let app: App | null = null;

function render(props: { harness: string; color?: boolean }): SVGSVGElement {
  const host = document.createElement('div');
  document.body.appendChild(host);
  app = createApp({ render: () => h(HarnessLogo, props) });
  app.mount(host);
  return host.querySelector('svg') as SVGSVGElement;
}

afterEach(() => {
  app?.unmount();
  app = null;
  document.body.innerHTML = '';
});

describe('HarnessLogo', () => {
  it('tints Claude with its brand color when color is set', () => {
    const svg = render({ harness: 'claude', color: true });
    expect(svg.getAttribute('fill')).toBe('#D97757');
  });

  it('keeps Claude monochrome (currentColor) without color', () => {
    const svg = render({ harness: 'claude' });
    expect(svg.getAttribute('fill')).toBe('currentColor');
  });

  it('keeps officially-monochrome marks as currentColor even with color', () => {
    for (const h of ['codex', 'cursor', 'opencode', 'pi']) {
      const svg = render({ harness: h, color: true });
      expect(svg.getAttribute('fill')).toBe('currentColor');
      app?.unmount();
      app = null;
    }
  });

  it('tints Topos with its brand color when color is set', () => {
    const svg = render({ harness: 'topos', color: true });
    expect(svg.getAttribute('fill')).toBe('#55707a');
  });

  it('renders the Topos node-graph mark (4 nodes + connecting path)', () => {
    const svg = render({ harness: 'topos' });
    expect(svg.getAttribute('fill')).toBe('currentColor');
    expect(svg.querySelectorAll('circle').length).toBe(4);
    expect(svg.querySelector('path')).not.toBeNull();
  });

  it('renders the fallback glyph for an unknown harness', () => {
    const svg = render({ harness: 'totally-unknown' });
    expect(svg.querySelector('circle')).not.toBeNull();
    expect(svg.querySelector('path')).toBeNull();
  });

  it('gives each codex blossom a distinct petal id so <use> refs never collide', () => {
    const host = document.createElement('div');
    document.body.appendChild(host);
    app = createApp({ render: () => h('div', [h(HarnessLogo, { harness: 'codex' }), h(HarnessLogo, { harness: 'codex' })]) });
    app.mount(host);
    const petals = Array.from(host.querySelectorAll('svg path[id]')).map((p) => p.getAttribute('id'));
    expect(petals).toHaveLength(2);
    expect(petals[0]).not.toBe(petals[1]);
    // every <use> points at a petal that exists in its own svg
    for (const use of Array.from(host.querySelectorAll('use'))) {
      const ref = use.getAttribute('href')!.slice(1);
      expect(host.querySelector(`#${CSS.escape(ref)}`)).not.toBeNull();
    }
  });
});
