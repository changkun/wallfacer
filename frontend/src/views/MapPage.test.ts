// MapPage is now a thin consumer of GET /api/graph drawn by GraphCanvas — no
// legacy window shims, no vendored renderer. These tests prove the new wiring:
// it fetches the graph and renders it, installs none of the old globals, and
// the archived toggle refetches with ?archived=1.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createRouter, createMemoryHistory, type Router } from 'vue-router';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import MapPage from './MapPage.vue';
import type { Graph } from '../api/types';

const graphFixture: Graph = {
  nodes: [
    { id: 'spec:a', kind: 'spec', label: 'Spec A', status: 'validated', ref: 'specs/a.md', depth: 0 },
    { id: 'task:b', kind: 'task', label: 'Task B', status: 'backlog', ref: 'b', depth: 0 },
  ],
  edges: [{ from: 'spec:a', to: 'task:b', kind: 'dispatch' }],
  critical_path: ['spec:a', 'task:b'],
  blocked: [],
};

interface Mounted {
  app: App;
  router: Router;
  host: HTMLElement;
}

let activePinia: Pinia;
let originalFetch: typeof globalThis.fetch;
const graphUrls: string[] = [];

async function mountMapPage(): Promise<Mounted> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/map', component: MapPage },
      { path: '/plan', component: { template: '<div />' } },
    ],
  });
  await router.push('/map');
  await router.isReady();

  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(MapPage);
  app.use(activePinia);
  app.use(router);
  app.mount(host);

  for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));
  return { app, router, host };
}

beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
  graphUrls.length = 0;
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url = typeof input === 'string' ? input : input.toString();
    if (url.includes('/api/graph')) {
      graphUrls.push(url);
      return new Response(JSON.stringify(graphFixture), { status: 200 });
    }
    if (url.includes('/api/tasks')) return new Response('[]', { status: 200 });
    return new Response('{}', { status: 200 });
  }) as unknown as typeof globalThis.fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

describe('MapPage', () => {
  it('fetches /api/graph and renders a node per graph node', async () => {
    const { app, host } = await mountMapPage();
    expect(graphUrls.some((u) => u.includes('/api/graph'))).toBe(true);
    expect(host.querySelectorAll('.gc-node').length).toBe(2);
    expect(host.querySelectorAll('.gc-edge').length).toBe(1);
    app.unmount();
    host.remove();
  });

  it('installs none of the legacy window shims', async () => {
    const { app, host } = await mountMapPage();
    const w = window as unknown as Record<string, unknown>;
    for (const k of ['specModeState', 'depGraphEnabled', 'openTaskModal', 'renderDependencyGraph', 'scheduleRender']) {
      expect(w[k]).toBeUndefined();
    }
    app.unmount();
    host.remove();
  });

  it('refetches with ?archived=1 when the archived toggle is checked', async () => {
    const { app, host } = await mountMapPage();
    const before = graphUrls.length;
    const checkbox = host.querySelector('input[type="checkbox"]') as HTMLInputElement;
    checkbox.checked = true;
    checkbox.dispatchEvent(new Event('change', { bubbles: true }));
    await nextTick();
    for (let i = 0; i < 3; i++) await new Promise((r) => setTimeout(r, 0));
    expect(graphUrls.length).toBeGreaterThan(before);
    expect(graphUrls.some((u) => u.includes('archived=1'))).toBe(true);
    app.unmount();
    host.remove();
  });
});
