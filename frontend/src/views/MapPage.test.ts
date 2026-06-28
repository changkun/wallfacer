// MapPage is now a thin consumer of GET /api/graph drawn by GraphCanvas — no
// legacy window shims, no vendored renderer. These tests prove the new wiring:
// it fetches the graph and renders it, installs none of the old globals, and
// the archived toggle refetches with ?archived=1.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createRouter, createMemoryHistory, type Router } from 'vue-router';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
// Stub the shared planning chat popup: its setup initializes chat sessions and
// threads, which is out of scope for these graph-wiring tests (and unmounted
// without a real backend). The Map only sets focus + calls .open() on it.
vi.mock('../components/plan/SpecChatPopup.vue', () => ({
  default: { name: 'SpecChatPopup', setup: () => ({ open() {} }), render: () => null },
}));

import MapPage from './MapPage.vue';
import type { Graph } from '../api/types';

const graphFixture: Graph = {
  nodes: [
    { id: 'spec:a', kind: 'spec', label: 'Spec A', status: 'validated', ref: 'specs/a.md', depth: 0, available_actions: ['dispatch'] },
    { id: 'task:b', kind: 'task', label: 'Task B', status: 'backlog', ref: 'b', depth: 0, available_actions: ['start'] },
    { id: 'task:d', kind: 'task', label: 'Task Done', status: 'done', ref: 'd', depth: 0 },
    { id: 'task:c', kind: 'task', label: 'Task Cancelled', status: 'cancelled', ref: 'c', depth: 0 },
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
    if (url.includes('/api/explorer/file')) {
      return new Response('# Spec A\n\nBody of the spec.', { status: 200 });
    }
    if (url.includes('/api/config')) {
      return new Response(JSON.stringify({ workspaces: ['/ws'] }), { status: 200 });
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

  it('fetches the task store on a cold mount so Board deep-jump works', async () => {
    // Landing on /map first leaves store.tasks empty; openInBoard/selectedTask
    // read it, so the page must populate it on mount (regression: a cold load
    // previously offered no Board jump for task nodes).
    const { app, host } = await mountMapPage();
    expect(calls.some((c) => c.url.includes('/api/tasks') && c.method === 'GET')).toBe(true);
    app.unmount();
    host.remove();
  });

  it('hides done and cancelled nodes by default', async () => {
    const { app, host } = await mountMapPage();
    // 4 graph nodes, but done + cancelled are hidden → 2 rendered.
    expect(host.querySelectorAll('.gc-node').length).toBe(2);
    const labels = [...host.querySelectorAll('.gc-node__label')].map((e) => e.textContent).join(' ');
    expect(labels).not.toContain('Done');
    expect(labels).not.toContain('Cancelled');
    app.unmount();
    host.remove();
  });

  it('toggles a state by clicking its legend row', async () => {
    const { app, host } = await mountMapPage();
    // The 'done' legend row starts in the off (filtered) state; clicking it
    // reveals the done node.
    const doneRow = [...host.querySelectorAll('.map-legend__item')].find((b) =>
      b.textContent?.toLowerCase().includes('done'),
    ) as HTMLButtonElement;
    expect(doneRow.classList.contains('map-legend__item--off')).toBe(true);
    doneRow.dispatchEvent(new Event('click', { bubbles: true }));
    await nextTick();
    expect(host.querySelectorAll('.gc-node').length).toBe(3); // done now visible
    expect(doneRow.classList.contains('map-legend__item--off')).toBe(false);
    // Clicking a visible state hides it.
    const backlogRow = [...host.querySelectorAll('.map-legend__item')].find((b) =>
      b.textContent?.toLowerCase().includes('backlog'),
    ) as HTMLButtonElement | undefined;
    if (backlogRow) {
      backlogRow.dispatchEvent(new Event('click', { bubbles: true }));
      await nextTick();
      expect(backlogRow.classList.contains('map-legend__item--off')).toBe(true);
    }
    app.unmount();
    host.remove();
  });

  it('focuses the spec when "Refine / discuss" is clicked', async () => {
    const { useAgentStore } = await import('../stores/agentSession');
    const { app, host } = await mountMapPage();
    const agent = useAgentStore();
    const spy = vi.spyOn(agent, 'focusSpec');
    await selectNode(host, 0); // spec:a
    const btn = buttonByText(host, 'Refine / discuss');
    expect(btn).not.toBeNull();
    btn!.dispatchEvent(new Event('click', { bubbles: true }));
    await nextTick();
    expect(spy).toHaveBeenCalledWith('specs/a.md');
    app.unmount();
    host.remove();
  });

  it('opens a spec popup on double-clicking a spec node', async () => {
    const { app, host } = await mountMapPage();
    const specNode = host.querySelector('.gc-node') as SVGGElement; // first = spec:a
    specNode.dispatchEvent(new Event('dblclick', { bubbles: true }));
    await nextTick();
    for (let i = 0; i < 4; i++) await new Promise((r) => setTimeout(r, 0));
    expect(host.querySelector('.map-popup')).not.toBeNull();
    expect(calls.some((c) => c.url.includes('/api/explorer/file'))).toBe(true);
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
