// Action buttons (Start, Mark as Done, …) fire an API call and rely on the
// task-updated SSE delta to swap the panel into its next state. Two regressions
// are pinned here:
//   1. Mark as Done must emit `close` (its hint promises "commit and close").
//   2. Buttons must guard against re-clicks while an action is in flight — the
//      user reported being able to hammer "Mark as Done" with no feedback,
//      which previously fired POST /done once per click.

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
let doneCalls = 0;

beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
  doneCalls = 0;
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url = typeof input === 'string' ? input : input.toString();
    if (url.includes('/done')) {
      doneCalls += 1;
      return new Response(JSON.stringify({ status: 'done' }), { status: 200 });
    }
    if (url.includes('/diff')) {
      return new Response(JSON.stringify({ diff: '', behind_counts: {} }), { status: 200 });
    }
    return new Response('[]', { status: 200 });
  }) as unknown as typeof globalThis.fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

async function settle() {
  for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));
}

async function mountDetail(task: Task) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div />' } }],
  });
  await router.push('/');
  await router.isReady();

  const host = document.createElement('div');
  document.body.appendChild(host);
  let closed = 0;
  const app = createApp(defineComponent({
    setup() {
      return () => h(TaskDetail, { task, initialTab: 'overview', onClose: () => { closed += 1; } });
    },
  }));
  app.use(activePinia);
  app.use(router);
  app.mount(host);
  await settle();
  return { app, host, closed: () => closed };
}

function markDoneButton(host: HTMLElement): HTMLButtonElement | undefined {
  return Array.from(host.querySelectorAll('button')).find(
    (b) => /mark as done/i.test(b.textContent || ''),
  ) as HTMLButtonElement | undefined;
}

describe('TaskDetail action buttons', () => {
  it('emits close after Mark as Done resolves', async () => {
    const { app, host, closed } = await mountDetail(makeTask('task-done'));

    const btn = markDoneButton(host);
    expect(btn).toBeTruthy();
    btn!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await settle();

    expect(doneCalls).toBe(1);
    expect(closed()).toBe(1);

    app.unmount();
    host.remove();
  });

  it('ignores re-clicks while the action is in flight', async () => {
    const { app, host } = await mountDetail(makeTask('task-guard'));

    const btn = markDoneButton(host);
    expect(btn).toBeTruthy();
    // Hammer the button before the first request settles. The in-flight guard
    // sets busyAction synchronously, so only the first click reaches /done.
    btn!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    btn!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    btn!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await settle();

    expect(doneCalls).toBe(1);

    app.unmount();
    host.remove();
  });
});
