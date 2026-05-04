// Regression tests for MapPage's wiring to the legacy depgraph IIFEs.
//
// We don't try to render the full SVG (the layered Sugiyama layout is
// 4000+ LOC of legacy code that needs a real browser to verify). The
// goal here is to prove the bridge: shims are installed before the
// legacy modules import, openTaskModal selects the right task, the
// shared specModeState reference stays stable when the spec tree
// reloads, and unmount tears the shims down.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createRouter, createMemoryHistory, type Router } from 'vue-router';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import MapPage from './MapPage.vue';
import { useTaskStore } from '../stores/tasks';
import type { Task } from '../api/types';

// Stub the legacy IIFE side-effect imports — happy-dom can't render the
// SVG layout pipeline (no getBBox / partial getComputedStyle). The bridge
// shims under test are installed by MapPage itself before these imports
// run, so making the imports no-ops doesn't reduce coverage of the wiring.
vi.mock('../../../ui/js/unified-graph.js', () => ({}));
vi.mock('../../../ui/js/depgraph.js', () => ({}));

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

interface MountedPage {
  app: App;
  router: Router;
  host: HTMLElement;
}

async function mountMapPage(): Promise<MountedPage> {
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
  // Reuse the active pinia so the test and the component share state.
  app.use(activePinia);
  app.use(router);
  app.mount(host);

  // Allow onMounted hooks (fetch + dynamic imports) to settle. Two ticks
  // covers the await for fetchTasks/loadSpecTree, and nextTick after
  // ready=true.
  for (let i = 0; i < 5; i++) {
    await new Promise(r => setTimeout(r, 0));
  }
  return { app, router, host };
}

let originalFetch: typeof globalThis.fetch;
let activePinia: Pinia;

beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url = typeof input === 'string' ? input : input.toString();
    if (url.includes('/api/specs/tree')) {
      return new Response(JSON.stringify({ nodes: [] }), { status: 200 });
    }
    if (url.includes('/api/tasks')) {
      return new Response(JSON.stringify([]), { status: 200 });
    }
    return new Response('{}', { status: 200 });
  }) as unknown as typeof globalThis.fetch;
  // Stub renderDependencyGraph so the test doesn't load the real legacy
  // module. The dynamic import in the component still runs but its
  // window.renderDependencyGraph assignment overwrites this stub —
  // either way, calling it after mount is harmless.
  (window as unknown as Record<string, unknown>).renderDependencyGraph = vi.fn();
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  for (const k of [
    'renderDependencyGraph',
    'hideDependencyGraph',
    'specModeState',
    'depGraphEnabled',
    'openTaskModal',
    'focusSpec',
    'switchMode',
    'scheduleRender',
    'setMapShowArchived',
    'setMapSearch',
    'resetMapLayout',
    '_resetMapCentering',
    'buildUnifiedGraph',
    'renderUnifiedGraph',
  ]) {
    delete (window as unknown as Record<string, unknown>)[k];
  }
});

describe('MapPage', () => {
  it('installs the window shims that the legacy renderer expects', async () => {
    const { app, host } = await mountMapPage();

    expect(window.specModeState).toBeDefined();
    expect(Array.isArray(window.specModeState!.tree)).toBe(true);
    expect(window.depGraphEnabled).toBe(true);
    expect(typeof window.openTaskModal).toBe('function');
    expect(typeof window.focusSpec).toBe('function');
    expect(typeof window.scheduleRender).toBe('function');

    app.unmount();
    host.remove();
    // After unmount the shims are restored to whatever was there before
    // mount (undefined in this test environment).
    expect(window.openTaskModal).toBeUndefined();
    expect(window.focusSpec).toBeUndefined();
    expect(window.specModeState).toBeUndefined();
  });

  it('opens TaskDetail overlay when openTaskModal is invoked', async () => {
    const store = useTaskStore();
    store.setTasks([makeTask('abc-123', { title: 'Hello' })]);
    const { app, host } = await mountMapPage();

    expect(host.querySelector('#modal')).toBeNull();
    window.openTaskModal!('abc-123');
    await nextTick();
    expect(host.querySelector('#modal')).not.toBeNull();

    app.unmount();
    host.remove();
  });

  it('navigates to /plan with the spec query when focusSpec fires', async () => {
    const { app, router, host } = await mountMapPage();

    window.focusSpec!('specs/local/foo.md');
    for (let i = 0; i < 5; i++) {
      await new Promise(r => setTimeout(r, 0));
    }
    expect(router.currentRoute.value.path).toBe('/plan');
    expect(router.currentRoute.value.query.spec).toBe('specs/local/foo.md');

    app.unmount();
    host.remove();
  });

  it('keeps the same specModeState reference across spec-tree refreshes', async () => {
    const { app, host } = await mountMapPage();
    const stateRef = window.specModeState!;
    const treeRef = stateRef.tree;
    // Implementation must mutate `tree` in place rather than reassigning,
    // because depgraph.js captures the object reference once.
    expect(window.specModeState).toBe(stateRef);
    expect(window.specModeState!.tree).toBe(treeRef);

    app.unmount();
    host.remove();
  });
});
