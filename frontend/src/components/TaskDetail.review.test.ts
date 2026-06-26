// End-to-end (component-level) check of the inline diff-review flow: on a waiting
// task in local mode (auth disabled → canReview true), a gutter opens an inline
// editor, saving stores a comment that the panel lists, and "Submit" POSTs one
// formatted feedback message under the `message` key and clears the store.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, defineComponent, h } from 'vue';
import { createRouter, createMemoryHistory } from 'vue-router';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import TaskDetail from './TaskDetail.vue';
import type { Task } from '../api/types';

function makeTask(id: string, overrides: Partial<Task> = {}): Task {
  return {
    id, title: `Task ${id}`, prompt: '', status: 'waiting', archived: false, result: null,
    stop_reason: null, turns: 0, timeout: 0,
    usage: { input_tokens: 0, output_tokens: 0, cache_read_input_tokens: 0, cache_creation_input_tokens: 0, cost_usd: 0 },
    sandbox: '', position: 0, created_at: '', updated_at: '', branch_name: '', commit_message: '',
    model: '', kind: '', tags: [], depends_on: [], failure_category: '', fresh_start: false,
    is_test_run: false, last_test_result: '', session_id: null, worktree_paths: {}, usage_breakdown: {},
    ...overrides,
  } as Task;
}

const DIFF = [
  'diff --git a/note.txt b/note.txt',
  '--- a/note.txt',
  '+++ b/note.txt',
  '@@ -1,2 +1,3 @@',
  ' keep',
  '+added line',
  ' tail',
].join('\n');

let activePinia: Pinia;
let originalFetch: typeof globalThis.fetch;
let feedbackBodies: Array<Record<string, unknown>> = [];

beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = typeof input === 'string' ? input : input.toString();
    if (url.includes('/feedback')) {
      feedbackBodies.push(init?.body ? JSON.parse(String(init.body)) : {});
      return new Response(JSON.stringify({ status: 'resumed' }), { status: 200 });
    }
    if (url.includes('/diff')) {
      return new Response(JSON.stringify({ diff: DIFF, behind_counts: {} }), { status: 200 });
    }
    if (url.includes('/api/config')) {
      return new Response(JSON.stringify({ auth_enabled: false }), { status: 200 });
    }
    if (url.includes('/api/me')) {
      return new Response('', { status: 204 });
    }
    return new Response('[]', { status: 200 });
  }) as unknown as typeof globalThis.fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
  feedbackBodies = [];
});

async function settle() {
  for (let i = 0; i < 10; i++) await new Promise((r) => setTimeout(r, 0));
}

function mount() {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(defineComponent({
    setup() {
      return () => h(TaskDetail, { task: makeTask('task-rev'), initialTab: 'changes' });
    },
  }));
  app.use(activePinia);
  app.use(createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] }));
  app.mount(host);
  return { host, app };
}

describe('TaskDetail inline review', () => {
  it('adds a line comment via the gutter and batch-submits it', async () => {
    const { host, app } = mount();
    await settle();

    // The added line carries a commentable gutter.
    const addedLine = Array.from(host.querySelectorAll<HTMLElement>('.diff-line'))
      .find((el) => (el.textContent || '').includes('added line'));
    expect(addedLine).toBeTruthy();
    const gutter = addedLine!.querySelector<HTMLButtonElement>('.dc-gutter');
    expect(gutter).toBeTruthy();

    gutter!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await settle();

    const textarea = host.querySelector<HTMLTextAreaElement>('.dc-editor .dc-editor-input');
    expect(textarea).toBeTruthy();
    textarea!.value = 'why was this added?';
    textarea!.dispatchEvent(new Event('input'));
    await settle();

    const saveBtn = Array.from(host.querySelectorAll<HTMLButtonElement>('.dc-editor .dc-btn'))
      .find((b) => /save/i.test(b.textContent || ''));
    saveBtn!.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await settle();

    // The panel now lists the comment.
    const panelBody = host.querySelector('.dc-item-body');
    expect(panelBody?.textContent).toContain('why was this added?');

    // Submit posts one formatted message under the message key.
    const submit = Array.from(host.querySelectorAll<HTMLButtonElement>('.dc-panel-foot button'))[0];
    expect(submit).toBeTruthy();
    submit.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    await settle();

    expect(feedbackBodies).toHaveLength(1);
    const msg = String(feedbackBodies[0].message);
    expect(msg).toContain('## Inline Review Comments');
    expect(msg).toContain('### note.txt');
    expect(msg).toContain('why was this added?');

    // Store cleared: the panel shows the empty prompt again.
    expect(host.querySelector('.dc-item-body')).toBeNull();

    app.unmount();
  });

  it('hides the review surface when auth is enabled and the browser is signed out', async () => {
    // Cloud mode (auth_enabled true) + signed-out (/api/me 204) → canReview false.
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.includes('/diff')) {
        return new Response(JSON.stringify({ diff: DIFF, behind_counts: {} }), { status: 200 });
      }
      if (url.includes('/api/config')) {
        return new Response(JSON.stringify({ auth_enabled: true }), { status: 200 });
      }
      if (url.includes('/api/me')) return new Response('', { status: 204 });
      return new Response('[]', { status: 200 });
    }) as unknown as typeof globalThis.fetch;

    const { host, app } = mount();
    // Pull config into the task store so authEnabled flips true.
    const { useTaskStore } = await import('../stores/tasks');
    await useTaskStore().fetchConfig();
    await settle();

    expect(host.querySelector('.dc-gutter')).toBeNull();
    expect(host.querySelector('.dc-panel')).toBeNull();
    // The diff itself still renders.
    expect(Array.from(host.querySelectorAll('.diff-line')).some((el) => (el.textContent || '').includes('added line'))).toBe(true);

    app.unmount();
  });
});
