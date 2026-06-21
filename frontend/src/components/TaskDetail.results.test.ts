// The Verification tab shows only the test/verify-phase transcript — turns at
// or after task.test_run_start_turn. Implementation turns live in Activity and
// the latest result in Spec, so they are not repeated here.

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

async function mountVerification(results: string[], overrides: Partial<Task> = {}): Promise<Mounted> {
  outputs = results;
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div />' } }],
  });
  await router.push('/');
  await router.isReady();

  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(TaskDetail, { task: makeTask('t1', overrides), initialTab: 'verification' });
  app.use(activePinia);
  app.use(router);
  app.mount(host);

  // Let onMounted -> fetchResults (async fetch) settle.
  for (let i = 0; i < 6; i++) await new Promise((r) => setTimeout(r, 0));
  return { app, host, router };
}

describe('TaskDetail Verification tab', () => {
  it('shows the test-phase transcript with turn labels', async () => {
    // test_run_start_turn=1 → every output belongs to the verification phase.
    const { app, host } = await mountVerification(
      ['VERIFY_TURN_ONE first.', 'VERIFY_TURN_TWO second.'],
      { test_run_start_turn: 1 } as Partial<Task>,
    );

    const section = host.querySelector('[data-main-tab-section="verification"]')!;
    expect(section.textContent).toContain('VERIFY_TURN_ONE');
    expect(section.textContent).toContain('VERIFY_TURN_TWO');
    expect(section.textContent).toContain('Turn 1');
    expect(section.textContent).toContain('Turn 2');
    expect(section.querySelectorAll('.result-entry').length).toBe(2);

    app.unmount();
  });

  it('shows an empty state when there is no verification run (impl-only turns are not repeated)', async () => {
    // No test_run_start_turn → the outputs are implementation turns, shown in
    // Activity/Spec instead, so Verification is empty.
    const { app, host } = await mountVerification(['implementation answer only.']);

    const section = host.querySelector('[data-main-tab-section="verification"]')!;
    expect(section.textContent).toContain('No verification run');
    expect(section.querySelector('.result-entry')).toBeNull();

    app.unmount();
  });
});
