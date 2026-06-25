// Regression test for the spec-tree sidebar resize splitter.
//
// We mount PlanPage with its child components and side-effecting imports
// (SSE, mermaid, API) stubbed, then drive the drag handle with synthetic
// mouse events and assert the panel width CSS var clamps as expected.
//
// happy-dom reports innerWidth as 0 and never lays out real geometry, so
// we only exercise the pure clamp math the handler runs against clientX
// deltas, not any layout-derived sizing.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createRouter, createMemoryHistory, type Router } from 'vue-router';
import { createPinia, setActivePinia } from 'pinia';

// Neutralize onMounted side effects: SSE connection, mermaid theme watcher,
// and the tree fetch through the API client.
vi.mock('../composables/useSse', () => ({ useSse: () => ({}) }));
vi.mock('../lib/mermaidRender', () => ({ watchThemeReinit: () => {} }));
vi.mock('../api/client', () => ({
  api: { get: vi.fn(async () => ({})), post: vi.fn(async () => ({})) },
}));

// Stub the three panes. SpecTreePanel renders the bound style so we can read
// back the --stp-width CSS var the splitter drives.
vi.mock('../components/plan/SpecTreePanel.vue', () => ({
  default: {
    name: 'SpecTreePanel',
    template: '<aside class="spec-tree-panel-stub"></aside>',
  },
}));
vi.mock('../components/plan/SpecFocusedView.vue', () => ({
  default: { name: 'SpecFocusedView', template: '<div></div>' },
}));
vi.mock('../components/plan/PlanningChatPanel.vue', () => ({
  default: { name: 'PlanningChatPanel', template: '<div></div>' },
}));
vi.mock('../components/plan/SpecChatPopup.vue', () => ({
  default: { name: 'SpecChatPopup', template: '<div></div>' },
}));

// happy-dom ships only a partial localStorage stub here, so install a small
// in-memory implementation the page and assertions can rely on.
const memStore = new Map<string, string>();
vi.stubGlobal('localStorage', {
  getItem: (k: string) => (memStore.has(k) ? memStore.get(k)! : null),
  setItem: (k: string, v: string) => { memStore.set(k, String(v)); },
  removeItem: (k: string) => { memStore.delete(k); },
  clear: () => { memStore.clear(); },
});

import PlanPage from './PlanPage.vue';
import { useAgentStore } from '../stores/agentSession';
import { useTaskStore } from '../stores/tasks';

interface Mounted {
  app: App;
  router: Router;
  host: HTMLElement;
}

async function mountPage(): Promise<Mounted> {
  setActivePinia(createPinia());
  // Give the planning store a tree so the layout resolves to three-pane
  // (which is what renders the splitter and SpecTreePanel).
  const planning = useAgentStore();
  planning.applyTree({
    nodes: [{ path: 'a', is_leaf: true, spec: { title: 'A', status: 'drafted' } } as never],
    index: null,
    progress: {},
  });
  const tasks = useTaskStore();
  tasks.config = { workspaces: ['/tmp/ws'] } as never;

  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/plan', component: PlanPage }],
  });
  router.push('/plan');
  await router.isReady();

  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(PlanPage);
  app.use(router);
  app.mount(host);
  await nextTick();
  return { app, router, host };
}

function drag(host: HTMLElement, fromX: number, toX: number) {
  const handle = host.querySelector('.plan-resize-handle') as HTMLElement;
  expect(handle).toBeTruthy();
  handle.dispatchEvent(new MouseEvent('mousedown', { clientX: fromX, bubbles: true }));
  document.dispatchEvent(new MouseEvent('mousemove', { clientX: toX, bubbles: true }));
  document.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
}

function stpWidth(host: HTMLElement): string {
  const panel = host.querySelector('.spec-tree-panel-stub') as HTMLElement;
  return panel.style.getPropertyValue('--stp-width');
}

describe('PlanPage sidebar resize', () => {
  beforeEach(() => {
    localStorage.removeItem('wallfacer-spec-sidebar-width');
  });
  afterEach(() => {
    document.body.innerHTML = '';
  });

  it('widens the panel when the handle is dragged right', async () => {
    const { host, app } = await mountPage();
    // Default 280; drag +100 → 380, within [200, 520].
    drag(host, 100, 200);
    await nextTick();
    expect(stpWidth(host)).toBe('380px');
    expect(localStorage.getItem('wallfacer-spec-sidebar-width')).toBe('380');
    app.unmount();
  });

  it('clamps the width to the 520px maximum', async () => {
    const { host, app } = await mountPage();
    // Drag far right; 280 + 1000 → clamps to 520.
    drag(host, 0, 1000);
    await nextTick();
    expect(stpWidth(host)).toBe('520px');
    app.unmount();
  });

  it('clamps the width to the 200px minimum', async () => {
    const { host, app } = await mountPage();
    // Drag far left; 280 - 1000 → clamps to 200.
    drag(host, 1000, 0);
    await nextTick();
    expect(stpWidth(host)).toBe('200px');
    app.unmount();
  });
});
