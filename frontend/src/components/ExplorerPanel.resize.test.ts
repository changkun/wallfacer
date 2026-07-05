// Regression test for the file-explorer resize splitter and its auto-fold.
//
// We mount ExplorerPanel with its API/SSE side effects stubbed and no workspace
// (so the tree load short-circuits), then drive the resize handle with pointer
// events. The width clamps to [200, 50vw]; dragging narrower than the fold
// threshold emits `close` so the parent snaps the panel to the rail instead of
// bottoming out at the min width.
//
// happy-dom reports no real geometry, so only the pure clamp/fold math is
// exercised against clientX deltas.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, h, nextTick, type App } from 'vue';
import { createRouter, createMemoryHistory } from 'vue-router';
import { createPinia, setActivePinia } from 'pinia';

vi.mock('../api/client', () => ({
  api: vi.fn(async () => ({})),
  withAuthToken: (u: string) => u,
}));

const memStore = new Map<string, string>();
vi.stubGlobal('localStorage', {
  getItem: (k: string) => (memStore.has(k) ? memStore.get(k)! : null),
  setItem: (k: string, v: string) => { memStore.set(k, String(v)); },
  removeItem: (k: string) => { memStore.delete(k); },
  clear: () => { memStore.clear(); },
});

import ExplorerPanel from './ExplorerPanel.vue';
import { useTaskStore } from '../stores/tasks';

interface Mounted { app: App; host: HTMLElement; closed: () => number }

async function mountPanel(): Promise<Mounted> {
  setActivePinia(createPinia());
  // No workspace → onMounted skips loadRoot; keeps the mount cheap and inert.
  const tasks = useTaskStore();
  tasks.config = { workspaces: [] } as never;

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div />' } }],
  });
  router.push('/');
  await router.isReady();

  let closeCount = 0;
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp({
    render: () => h(ExplorerPanel, { onClose: () => { closeCount += 1; } }),
  });
  app.use(router);
  app.mount(host);
  await nextTick();
  return { app, host, closed: () => closeCount };
}

function drag(host: HTMLElement, fromX: number, toX: number) {
  const handle = host.querySelector('.explorer-panel__resize-handle') as HTMLElement;
  expect(handle).toBeTruthy();
  handle.dispatchEvent(new MouseEvent('pointerdown', { clientX: fromX, bubbles: true }));
  window.dispatchEvent(new MouseEvent('pointermove', { clientX: toX, bubbles: true }));
  window.dispatchEvent(new MouseEvent('pointerup', { bubbles: true }));
}

function panelWidth(host: HTMLElement): string {
  const panel = host.querySelector('.explorer-panel') as HTMLElement;
  return panel.style.width;
}

describe('ExplorerPanel resize', () => {
  beforeEach(() => {
    memStore.clear();
  });
  afterEach(() => {
    document.body.innerHTML = '';
  });

  it('widens the panel when the handle is dragged right', async () => {
    const { host, app, closed } = await mountPanel();
    // Default 260; drag +100 → 360.
    drag(host, 100, 200);
    await nextTick();
    expect(panelWidth(host)).toBe('360px');
    expect(closed()).toBe(0);
    app.unmount();
  });

  it('clamps to the 200px minimum for a small leftward drag', async () => {
    const { host, app, closed } = await mountPanel();
    // 260 - 80 = 180 → above the 150 fold threshold, clamps up to 200.
    drag(host, 100, 20);
    await nextTick();
    expect(panelWidth(host)).toBe('200px');
    expect(closed()).toBe(0);
    app.unmount();
  });

  it('auto-folds (emits close) when dragged past the fold threshold', async () => {
    const { host, app, closed } = await mountPanel();
    // 260 - 150 = 110 → below the 150 threshold: emit close.
    drag(host, 200, 50);
    await nextTick();
    expect(closed()).toBe(1);
    app.unmount();
  });
});
