// The Overview-tab feedback submit must POST the text under the `message` key,
// the key the SubmitFeedback handler decodes (json:"message"). It previously sent
// `feedback`, leaving message empty so the handler 400'd ("message is required").

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, defineComponent, h } from 'vue';
import { createRouter, createMemoryHistory } from 'vue-router';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import TaskDetail from './TaskDetail.vue';
import type { Task } from '../api/types';

function makeTask(id: string, overrides: Partial<Task> = {}): Task {
  return {
    id,
    title: `Task ${id}`,
    prompt: '',
    status: 'waiting',
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

let activePinia: Pinia;
let originalFetch: typeof globalThis.fetch;
let feedbackBodies: unknown[] = [];

beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = typeof input === 'string' ? input : input.toString();
    if (url.includes('/feedback')) {
      feedbackBodies.push(init?.body ? JSON.parse(String(init.body)) : null);
      return new Response(JSON.stringify({ status: 'resumed' }), { status: 200 });
    }
    if (url.includes('/diff')) {
      return new Response(JSON.stringify({ diff: '', behind_counts: {} }), { status: 200 });
    }
    return new Response('[]', { status: 200 });
  }) as unknown as typeof globalThis.fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  feedbackBodies = [];
});

async function settle() {
  for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));
}

describe('TaskDetail feedback submit', () => {
  it('POSTs the textarea text under the message key', async () => {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    });
    await router.push('/');
    await router.isReady();

    const host = document.createElement('div');
    document.body.appendChild(host);
    const app = createApp(defineComponent({
      setup() {
        return () => h(TaskDetail, { task: makeTask('task-fb'), initialTab: 'overview' });
      },
    }));
    app.use(activePinia);
    app.use(router);
    app.mount(host);
    await settle();

    const textarea = host.querySelector('textarea');
    expect(textarea).toBeTruthy();
    (textarea as HTMLTextAreaElement).value = 'please fix the race';
    textarea!.dispatchEvent(new Event('input'));
    await settle();

    // Find the Submit Feedback button and click it.
    const btn = Array.from(host.querySelectorAll('button')).find(
      (b) => /submit feedback/i.test(b.textContent || ''),
    );
    expect(btn).toBeTruthy();
    btn!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await settle();

    expect(feedbackBodies).toHaveLength(1);
    expect(feedbackBodies[0]).toEqual({ message: 'please fix the race' });

    app.unmount();
  });
});
