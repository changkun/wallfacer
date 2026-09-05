import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, defineComponent, h, nextTick } from 'vue';
import { createRouter, createMemoryHistory } from 'vue-router';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import TaskDetail from './TaskDetail.vue';
import type { Task } from '../api/types';

// The sheet (specs/shared/console-redesign/task-detail.md): one ink action
// per status, ghosts for the rest, Delete in the card foot, and one visible
// tab section at a time.
function makeTask(over: Partial<Task> = {}): Task {
  return {
    id: 't1', title: 'Task', prompt: 'p', status: 'backlog', archived: false, result: null, stop_reason: null,
    turns: 0, timeout: 0, usage: { input_tokens: 0, output_tokens: 0, cache_read_input_tokens: 0, cache_creation_input_tokens: 0, cost_usd: 0 },
    sandbox: '', position: 0, created_at: '', updated_at: '', branch_name: '', commit_message: '', model: '', kind: '',
    tags: [], depends_on: [], failure_category: '', fresh_start: false, is_test_run: false, last_test_result: '',
    session_id: null, worktree_paths: {}, usage_breakdown: {}, ...over,
  } as Task;
}

let activePinia: Pinia;
let originalFetch: typeof globalThis.fetch;
beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === 'string' ? input : input.toString();
    if (url.includes('/diff')) return new Response(JSON.stringify({ diff: '', behind_counts: {} }), { status: 200 });
    return new Response('[]', { status: 200 });
  }) as unknown as typeof globalThis.fetch;
});
afterEach(() => { globalThis.fetch = originalFetch; });

async function mount(task: Task) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }] });
  await router.push('/'); await router.isReady();
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(defineComponent({ setup: () => () => h(TaskDetail, { task, initialTab: 'overview', onClose: () => {} }) }));
  app.use(activePinia); app.use(router); app.mount(host);
  for (let i = 0; i < 6; i++) await new Promise((r) => setTimeout(r, 0));
  return { app, host };
}
const ink = (host: HTMLElement) => Array.from(host.querySelectorAll<HTMLButtonElement>('.sheet-actions .btn:not(.ghost)')).map((b) => b.dataset.action);
const ghosts = (host: HTMLElement) => Array.from(host.querySelectorAll<HTMLButtonElement>('.sheet-actions .btn.ghost')).map((b) => b.dataset.action);

describe('TaskDetail sheet', () => {
  it.each([
    ['backlog', {}, ['start'], ['edit']],
    ['waiting', { session_id: 's' }, ['done'], ['test', 'review', 'sync', 'cancel']],
    ['failed', { session_id: 's' }, ['resume'], ['test', 'sync', 'retry']],
    ['done', {}, [], ['test', 'archive']],
  ] as const)('%s: one ink action, the rest ghosts', async (status, over, wantInk, wantGhost) => {
    const { app, host } = await mount(makeTask({ status: status as Task['status'], ...over }));
    expect(ink(host)).toEqual(wantInk);
    expect(ghosts(host)).toEqual(wantGhost);
    expect(host.querySelector('.sheet-aside .card-foot [data-action="delete"]')!.classList.contains('danger')).toBe(true);
    app.unmount(); host.remove();
  });

  it('shows the state pill in the head and one section per tab', async () => {
    const { app, host } = await mount(makeTask({ status: 'waiting' }));
    expect(host.querySelector('[data-role="state"]')!.textContent).toBe('waiting');
    const sheet = host.querySelector('.sheet')!;
    expect(sheet.getAttribute('data-main-tab')).toBe('spec');
    (host.querySelector('[data-tab="events"]') as HTMLButtonElement).click();
    await nextTick();
    expect(sheet.getAttribute('data-main-tab')).toBe('events');
    expect(host.querySelector('[data-tab="events"]')!.classList.contains('on')).toBe(true);
    expect(host.querySelectorAll('.tab.on').length).toBe(1);
    app.unmount(); host.remove();
  });
});
