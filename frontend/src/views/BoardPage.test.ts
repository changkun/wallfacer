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

beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
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
