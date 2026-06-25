// The Changes tab caches its diff with a diffFetched flag. When the user
// switches tasks while the tab stays open (the TaskDetail instance is reused,
// with no :key), the per-task watcher must reset that flag and refetch, or the
// diff stays pinned to the previous task.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, defineComponent, h, ref } from 'vue';
import { createRouter, createMemoryHistory } from 'vue-router';
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

let activePinia: Pinia;
let originalFetch: typeof globalThis.fetch;
let diffCalls: string[] = [];

beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url = typeof input === 'string' ? input : input.toString();
    if (url.includes('/diff')) {
      diffCalls.push(url);
      return new Response(JSON.stringify({ diff: '', behind_counts: {} }), { status: 200 });
    }
    return new Response('[]', { status: 200 });
  }) as unknown as typeof globalThis.fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  diffCalls = [];
});

async function settle() {
  for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));
}

describe('TaskDetail Changes tab', () => {
  it('refetches the diff when the task changes while the tab stays open', async () => {
    const task = ref(makeTask('task-aaa'));
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    });
    await router.push('/');
    await router.isReady();

    const host = document.createElement('div');
    document.body.appendChild(host);
    const Wrapper = defineComponent({
      setup() {
        return () => h(TaskDetail, { task: task.value, initialTab: 'changes' });
      },
    });
    const app = createApp(Wrapper);
    app.use(activePinia);
    app.use(router);
    app.mount(host);

    await settle();
    expect(diffCalls.filter((u) => u.includes('task-aaa'))).toHaveLength(1);

    // Switch to a different task while the Changes tab is open.
    task.value = makeTask('task-bbb');
    await settle();

    expect(diffCalls.filter((u) => u.includes('task-bbb'))).toHaveLength(1);

    app.unmount();
  });
});
