// Regression test for the Open Explorer toggle.
//
// The file explorer used to be a standalone /explorer route, so opening it
// navigated away and hid the board entirely (see
// specs/foundations/file-explorer.md — it was always meant to be a left side
// panel with the board still visible). This test pins the corrected behaviour:
// the folder button toggles an in-board ExplorerPanel without a route change,
// and the board grid stays mounted alongside it.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createRouter, createMemoryHistory, type Router } from 'vue-router';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import BoardPage from './BoardPage.vue';
import { useTaskStore } from '../stores/tasks';
import { useUiStore } from '../stores/ui';
import type { Task } from '../api/types';

function makeTask(id: string, overrides: Partial<Task> = {}): Task {
  return {
    id,
    title: `Task ${id}`,
    prompt: '',
    status: 'backlog',
    archived: false,
    result: null,
    stop_reason: null,
    turns: 0,
    timeout: 0,
    usage: { input_tokens: 0, output_tokens: 0, cache_read_input_tokens: 0, cache_creation_input_tokens: 0, cost_usd: 0 },
    sandbox: '',
    position: 0,
    created_at: '',
    updated_at: '',
    branch_name: '',
    commit_message: '',
    model: '',
    kind: '',
    tags: [],
    depends_on: [],
    failure_category: '',
    fresh_start: false,
    is_test_run: false,
    last_test_result: '',
    session_id: null,
    worktree_paths: {},
    usage_breakdown: {},
    ...overrides,
  };
}

interface Mounted {
  app: App;
  router: Router;
  host: HTMLElement;
}

async function mountBoard(): Promise<Mounted> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: BoardPage },
      { path: '/settings', component: { template: '<div />' } },
    ],
  });
  await router.push('/');
  await router.isReady();

  const host = document.createElement('div');
  document.body.appendChild(host);

  const app = createApp(BoardPage);
  app.use(activePinia);
  app.use(router);
  app.mount(host);

  for (let i = 0; i < 5; i++) await new Promise((r) => setTimeout(r, 0));
  return { app, router, host };
}

let originalFetch: typeof globalThis.fetch;
let activePinia: Pinia;
let storage: Map<string, string>;

beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
  storage = new Map();
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => {
      storage.set(key, value);
    },
    removeItem: (key: string) => storage.delete(key),
    clear: () => storage.clear(),
    key: (index: number) => Array.from(storage.keys())[index] ?? null,
    get length() {
      return storage.size;
    },
  });
  // happy-dom lacks IntersectionObserver, which BoardPage's column observer
  // constructs on mount.
  (globalThis as unknown as Record<string, unknown>).IntersectionObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (): Promise<Response> => new Response('[]', { status: 200 })) as unknown as typeof globalThis.fetch;

  // Seed the store so the board grid renders (not the empty/needs-workspace
  // states): a workspace config plus one task.
  const store = useTaskStore();
  store.config = { workspaces: ['/repo'], max_parallel: 5 } as never;
  store.setTasks([makeTask('t-1')]);
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  vi.unstubAllGlobals();
});

describe('BoardPage Open Explorer toggle', () => {
  it('toggles the explorer panel in-place without leaving the board', async () => {
    const ui = useUiStore();
    const { app, router, host } = await mountBoard();

    // Board grid is present; explorer is hidden by default.
    expect(host.querySelector('#board')).not.toBeNull();
    expect(host.querySelector('.explorer-panel')).toBeNull();
    expect(ui.showExplorer).toBe(false);

    // The toggle is a button, not a navigating link.
    const btn = host.querySelector<HTMLButtonElement>('button.settings-btn');
    expect(btn).not.toBeNull();

    btn!.click();
    await nextTick();
    await nextTick();

    // Panel appears, board stays mounted, and the route never changed.
    expect(ui.showExplorer).toBe(true);
    expect(host.querySelector('.explorer-panel')).not.toBeNull();
    expect(host.querySelector('#board')).not.toBeNull();
    expect(router.currentRoute.value.path).toBe('/');

    // Toggling again hides the panel; board still there.
    btn!.click();
    await nextTick();
    expect(ui.showExplorer).toBe(false);
    expect(host.querySelector('.explorer-panel')).toBeNull();
    expect(host.querySelector('#board')).not.toBeNull();

    app.unmount();
    host.remove();
  });
});

describe('BoardPage detail panel reflects live SSE updates', () => {
  // Regression: clicking "Start" (or any action) PATCHes the task and the new
  // status arrives via the /api/tasks/stream `task-updated` SSE event, which
  // calls store.updateTask(). The open TaskDetail panel must reflect the new
  // status — badge swaps, the Start button disappears — otherwise the user
  // gets zero visual feedback and can re-click stale actions forever.
  it('updates the open task detail when the store task is replaced', async () => {
    const store = useTaskStore();
    const { app, router, host } = await mountBoard();

    // Open the backlog task's detail panel via deep link (?task=).
    await router.push('/?task=t-1');
    await router.isReady();
    for (let i = 0; i < 5; i++) await nextTick();

    const labels = () =>
      Array.from(host.querySelectorAll('.aside-action__label')).map((n) => n.textContent?.trim());
    const badgeText = () => host.querySelector('#modal .badge')?.textContent?.trim();

    // Backlog state: "Start task" action is offered, badge reads "backlog".
    expect(labels()).toContain('Start task');
    expect(badgeText()).toBe('backlog');

    // Simulate the SSE `task-updated` delta moving the task to in_progress.
    store.updateTask(makeTask('t-1', { status: 'in_progress' }));
    for (let i = 0; i < 5; i++) await nextTick();

    // Panel must now reflect in_progress: no Start action, badge updated.
    expect(labels()).not.toContain('Start task');
    expect(badgeText()).toBe('in_progress');

    app.unmount();
    host.remove();
  });
});
