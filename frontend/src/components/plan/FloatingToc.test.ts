// FloatingToc surfaces the spec's headings as a floating nav and can be
// hidden. These pin the toggle contract: a collapse button swaps the panel for
// a small reveal tab, the choice persists to localStorage, and a persisted
// collapsed state is honoured on mount.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, h, type App } from 'vue';
import FloatingToc from './FloatingToc.vue';

const memStore = new Map<string, string>();
vi.stubGlobal('localStorage', {
  getItem: (k: string) => (memStore.has(k) ? memStore.get(k)! : null),
  setItem: (k: string, v: string) => { memStore.set(k, String(v)); },
  removeItem: (k: string) => { memStore.delete(k); },
  clear: () => { memStore.clear(); },
});

// jsdom has no IntersectionObserver; the component constructs one when it finds
// headings, so stub a no-op.
vi.stubGlobal('IntersectionObserver', class {
  observe() {}
  unobserve() {}
  disconnect() {}
});

const KEY = 'wallfacer-spec-toc-collapsed';

let app: App | null = null;
let host: HTMLElement;

function bodyWithHeadings(): HTMLElement {
  const el = document.createElement('div');
  el.innerHTML = '<h2>Alpha</h2><p>x</p><h3>Beta</h3>';
  document.body.appendChild(el);
  return el;
}

async function mount(bodyEl: HTMLElement | null): Promise<void> {
  host = document.createElement('div');
  document.body.appendChild(host);
  app = createApp({ render: () => h(FloatingToc, { bodyEl, contentKey: 'k1' }) });
  app.mount(host);
  await nextTick();
  await nextTick(); // rebuild runs in a queued nextTick
}

beforeEach(() => { memStore.clear(); });
afterEach(() => {
  app?.unmount();
  app = null;
  document.body.innerHTML = '';
});

describe('FloatingToc collapse toggle', () => {
  it('shows the panel with entries by default', async () => {
    await mount(bodyWithHeadings());
    expect(host.querySelector('.floating-toc')).not.toBeNull();
    expect(host.querySelector('.floating-toc__reveal')).toBeNull();
    expect(host.querySelectorAll('.floating-toc__entry')).toHaveLength(2);
  });

  it('collapses to a reveal tab and persists the choice', async () => {
    await mount(bodyWithHeadings());
    (host.querySelector('.floating-toc__collapse') as HTMLElement).click();
    await nextTick();
    expect(host.querySelector('.floating-toc')).toBeNull();
    expect(host.querySelector('.floating-toc__reveal')).not.toBeNull();
    expect(memStore.get(KEY)).toBe('1');
  });

  it('reveals the panel again from the tab', async () => {
    await mount(bodyWithHeadings());
    (host.querySelector('.floating-toc__collapse') as HTMLElement).click();
    await nextTick();
    (host.querySelector('.floating-toc__reveal') as HTMLElement).click();
    await nextTick();
    expect(host.querySelector('.floating-toc')).not.toBeNull();
    expect(memStore.get(KEY)).toBe('0');
  });

  it('honours a persisted collapsed state on mount', async () => {
    memStore.set(KEY, '1');
    await mount(bodyWithHeadings());
    expect(host.querySelector('.floating-toc')).toBeNull();
    expect(host.querySelector('.floating-toc__reveal')).not.toBeNull();
  });

  it('renders nothing when the body has no headings', async () => {
    const empty = document.createElement('div');
    document.body.appendChild(empty);
    await mount(empty);
    expect(host.querySelector('.floating-toc')).toBeNull();
    expect(host.querySelector('.floating-toc__reveal')).toBeNull();
  });
});
