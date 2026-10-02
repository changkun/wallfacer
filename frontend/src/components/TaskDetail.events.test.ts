// The Events tab lists one row per task event with a one-line summary. A
// system event carries its sentence in `result` (or `message`), with `kind`
// as a machine label on some of them; the row shows the sentence when there
// is one and falls back to the kind only when there is none.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, type App } from 'vue';
import { createRouter, createMemoryHistory } from 'vue-router';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import TaskDetail from './TaskDetail.vue';
import type { Task } from '../api/types';

function makeTask(id: string): Task {
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
  } as Task;
}

const FLEET_SENTENCE = 'The fleet "reviewers" this task names no longer exists, so the task runs on the built-in pipeline.';
const COMMIT_SENTENCE = 'Phase 1/3: Staging and committing changes...';
const BUDGET_SENTENCE = 'cost budget of $1.00 exceeded';

const EVENTS = [
  { id: 1, event_type: 'system', created_at: '', data: { kind: 'fleet:removed', flow_id: 'reviewers', result: FLEET_SENTENCE } },
  { id: 2, event_type: 'system', created_at: '', data: { result: COMMIT_SENTENCE } },
  { id: 3, event_type: 'system', created_at: '', data: { message: BUDGET_SENTENCE, budget_exceeded: true } },
  { id: 4, event_type: 'system', created_at: '', data: { kind: 'routine:fired', instance: 'abc' } },
];

let activePinia: Pinia;
let originalFetch: typeof globalThis.fetch;

beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url = typeof input === 'string' ? input : input.toString();
    if (url.endsWith('/events')) {
      return new Response(JSON.stringify(EVENTS), { status: 200 });
    }
    return new Response('[]', { status: 200 });
  }) as unknown as typeof globalThis.fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

async function mountEvents(): Promise<{ app: App; host: HTMLElement }> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div />' } }],
  });
  await router.push('/');
  await router.isReady();

  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(TaskDetail, { task: makeTask('t1'), initialTab: 'events' });
  app.use(activePinia);
  app.use(router);
  app.mount(host);

  // Let onMounted -> fetchEvents (async fetch) settle.
  for (let i = 0; i < 6; i++) await new Promise((r) => setTimeout(r, 0));
  return { app, host };
}

function summaries(host: HTMLElement): string[] {
  return Array.from(host.querySelectorAll('[data-main-tab-section="events"] .event-row__summary'))
    .map((el) => (el.textContent || '').trim());
}

describe('TaskDetail Events tab', () => {
  it('shows the sentence of a system event, and its kind only when there is no sentence', async () => {
    const { app, host } = await mountEvents();

    const rows = summaries(host);
    expect(rows).toHaveLength(4);
    expect(rows[0]).toContain('no longer exists');
    expect(rows[0]).not.toBe('fleet:removed');
    expect(rows[1]).toBe(COMMIT_SENTENCE);
    expect(rows[2]).toBe(BUDGET_SENTENCE);
    expect(rows[3]).toBe('routine:fired');

    app.unmount();
  });
});
