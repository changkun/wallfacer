// Regression test for the Results tab single-turn "empty box" bug.
//
// The bug: each turn rendered as a <details>/<summary>. For a single,
// non-plan turn the summary had no Plan badge and no "Turn N" label, so it
// drew an empty clickable row with only a stray triangle (the
// .result-entry-labels::before marker) sitting above the real content.
//
// The fix: render single non-plan turns flat (a plain .result-entry div, no
// .result-entry-summary), while 2+ turns keep the collapsible summaries.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, type App } from 'vue';
import { createRouter, createMemoryHistory, type Router } from 'vue-router';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import TaskDetail from './TaskDetail.vue';
import type { Task } from '../api/types';

function makeTask(id: string, overrides: Partial<Task> = {}): Task {
  return {
    id,
    title: `Task ${id}`,
    prompt: '',
    status: 'done',
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
  } as Task;
}

// Build an /events?type=output payload from a list of result strings.
function outputEvents(results: string[]) {
  return results.map((result, i) => ({
    id: `ev-${i}`,
    event_type: 'output',
    created_at: '',
    data: { result },
  }));
}

let activePinia: Pinia;
let originalFetch: typeof globalThis.fetch;
let outputs: string[] = [];

beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url = typeof input === 'string' ? input : input.toString();
    if (url.includes('type=output')) {
      return new Response(JSON.stringify(outputEvents(outputs)), { status: 200 });
    }
    return new Response('[]', { status: 200 });
  }) as unknown as typeof globalThis.fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  outputs = [];
});

interface Mounted { app: App; host: HTMLElement; router: Router }

async function mountResults(results: string[]): Promise<Mounted> {
  outputs = results;
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div />' } }],
  });
  await router.push('/');
  await router.isReady();

  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(TaskDetail, { task: makeTask('t1'), initialTab: 'results' });
  app.use(activePinia);
  app.use(router);
  app.mount(host);

  // Let onMounted -> fetchResults (async fetch) settle.
  for (let i = 0; i < 6; i++) await new Promise((r) => setTimeout(r, 0));
  return { app, host, router };
}

describe('TaskDetail Results tab', () => {
  it('renders a single non-plan turn flat, without an empty summary box', async () => {
    const { app, host } = await mountResults(['UNIQUE_SINGLE_TURN_TEXT done.']);

    const results = host.querySelector('[data-main-tab-section="results"]')!;
    // The content is present...
    expect(results.textContent).toContain('UNIQUE_SINGLE_TURN_TEXT');
    // ...but no clickable collapsible summary row (the empty box) is rendered.
    expect(results.querySelector('.result-entry-summary')).toBeNull();
    // The entry still exists as a flat container.
    expect(results.querySelector('.result-entry')).not.toBeNull();

    app.unmount();
  });

  it('keeps collapsible summaries for multi-turn results', async () => {
    const { app, host } = await mountResults([
      'TURN_ONE_TEXT first.',
      'TURN_TWO_TEXT second.',
    ]);

    const results = host.querySelector('[data-main-tab-section="results"]')!;
    const summaries = results.querySelectorAll('.result-entry-summary');
    expect(summaries.length).toBe(2);
    // Turn labels appear in the multi-turn case.
    expect(results.textContent).toContain('Turn 1');
    expect(results.textContent).toContain('Turn 2');

    app.unmount();
  });
});
