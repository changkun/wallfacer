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
    { id: 'spec:a', kind: 'spec', label: 'Spec A', status: 'validated', ref: 'specs/a.md', depth: 0, available_actions: ['dispatch'] },
    { id: 'task:b', kind: 'task', label: 'Task B', status: 'backlog', ref: 'b', depth: 0, available_actions: ['start'] },
  ],
  edges: [{ from: 'spec:a', to: 'task:b', kind: 'dispatch' }],
  critical_path: ['spec:a', 'task:b'],
  blocked: [],
};

interface Call { method: string; url: string; body: unknown }
const calls: Call[] = [];

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
  calls.length = 0;
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = typeof input === 'string' ? input : input.toString();
    const method = (init?.method ?? 'GET').toUpperCase();
    let body: unknown;
    try { body = init?.body ? JSON.parse(init.body as string) : undefined; } catch { body = init?.body; }
    calls.push({ method, url, body });
    if (url.includes('/api/graph')) {
      graphUrls.push(url);
      return new Response(JSON.stringify(graphFixture), { status: 200 });
    }
    if (url.includes('/api/specs/transition')) {
      return new Response(JSON.stringify({ dispatched: [{ task_id: 'new-task' }] }), { status: 200 });
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

  function buttonByText(host: HTMLElement, text: string): HTMLButtonElement | null {
    return ([...host.querySelectorAll('button')] as HTMLButtonElement[]).find(
      (b) => b.textContent?.trim() === text,
    ) ?? null;
  }

  async function selectNode(host: HTMLElement, index: number) {
    const node = host.querySelectorAll('.gc-node')[index] as SVGGElement;
    node.dispatchEvent(new Event('click', { bubbles: true }));
    await nextTick();
  }

  it('dispatches a validated leaf spec and refetches the graph', async () => {
    const { app, host } = await mountMapPage();
    await selectNode(host, 0); // spec:a (available_actions: dispatch)
    const before = graphUrls.length;
    const btn = buttonByText(host, 'Dispatch');
    expect(btn).not.toBeNull();
    btn!.dispatchEvent(new Event('click', { bubbles: true }));
    for (let i = 0; i < 4; i++) await new Promise((r) => setTimeout(r, 0));

    const dispatch = calls.find((c) => c.url.includes('/api/specs/transition'));
    expect(dispatch?.method).toBe('POST');
    expect((dispatch?.body as { action: string }).action).toBe('dispatch');
    expect(graphUrls.length).toBeGreaterThan(before); // re-synced after action
    app.unmount();
    host.remove();
  });

  it('starts a ready backlog task via PATCH in_progress', async () => {
    const { app, host } = await mountMapPage();
    await selectNode(host, 1); // task:b (available_actions: start)
    const btn = buttonByText(host, 'Start');
    expect(btn).not.toBeNull();
    btn!.dispatchEvent(new Event('click', { bubbles: true }));
    for (let i = 0; i < 4; i++) await new Promise((r) => setTimeout(r, 0));

    const patch = calls.find((c) => c.url.includes('/api/tasks/b') && c.method === 'PATCH');
    expect(patch).toBeTruthy();
    expect((patch?.body as { status: string }).status).toBe('in_progress');
    app.unmount();
    host.remove();
  });

  it('lists actionable nodes under "Ready to act"', async () => {
    const { app, host } = await mountMapPage();
    const ready = host.querySelectorAll('.depgraph-inspector__ready-item');
    expect(ready.length).toBe(2); // both fixture nodes carry an action
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
