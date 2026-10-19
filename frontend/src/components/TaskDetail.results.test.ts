// The Verification tab shows the review only. Implementation turns live in
// Activity and the latest result in Spec, so they are not repeated here.

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
    failure_category: '',
    fresh_start: false,
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
  it('shows an empty state when there is no review (implementation turns are not repeated)', async () => {
    // The outputs are implementation turns, shown in Activity/Spec instead,
    // so Verification shows only the review's empty state.
    const { app, host } = await mountVerification(['implementation answer only.']);

    const section = host.querySelector('[data-main-tab-section="verification"]')!;
    expect(section.textContent).toContain('No review has run');
    expect(section.textContent).not.toContain('implementation answer only.');

    app.unmount();
  });
});
