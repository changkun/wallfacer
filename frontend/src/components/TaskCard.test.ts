import { describe, expect, it, vi } from 'vitest';
import { createApp, nextTick } from 'vue';
import { createRouter, createMemoryHistory } from 'vue-router';
import { createPinia } from 'pinia';

// The card's colour budget (specs/shared/console-redesign/board.md): one
// state pill and at most one qualifier pill on the first row, tags as a mono
// meta line, and one ink action per column.
vi.mock('../api/client', () => ({ api: vi.fn(async () => ({})), ApiError: class extends Error {} }));

import TaskCard from './TaskCard.vue';
import type { Task } from '../api/types';

function task(over: Partial<Task>): Task {
  return {
    id: 't1', title: 'Wire dependency badges', prompt: 'Show a badge', status: 'backlog',
    created_at: new Date().toISOString(), updated_at: new Date().toISOString(), timeout: 60, turns: 0,
    tags: [], ...over,
  } as Task;
}

async function mountCard(t: Task, rank?: number) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }, { path: '/:rest(.*)', component: { template: '<div />' } }] });
  await router.push('/'); await router.isReady();
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(TaskCard, { task: t, rank });
  app.use(createPinia()); app.use(router);
  app.mount(host);
  await nextTick();
  return { host, app };
}

describe('TaskCard', () => {
  it('renders one state pill and at most one qualifier', async () => {
    const { host, app } = await mountCard(task({ status: 'failed', last_test_result: 'fail', failure_category: 'agent_error', session_id: 's' }));
    expect(host.querySelector('[data-role="state"]')!.classList.contains('pill-err')).toBe(true);
    expect(host.querySelectorAll('.task-card__pills .pill').length).toBeLessThanOrEqual(2);
    expect(host.querySelector('[data-role="qualifier"]')!.textContent).toContain('verify failed');
    // The failure category did not vanish: it moved to the meta line.
    expect(host.querySelector('.task-card__tags')!.textContent).toContain('Agent Error');
    app.unmount(); host.remove();
  });

  it('puts tags on one meta line with priority first and only high coloured', async () => {
    const { host, app } = await mountCard(task({ tags: ['frontend', 'impact:3', 'priority:high'] }));
    const tags = Array.from(host.querySelectorAll('.task-card__tag')).map((e) => e.textContent!.trim());
    expect(tags).toEqual(['high', 'impact 3', 'frontend']);
    expect(host.querySelector('[data-tag="priority:high"]')!.classList.contains('task-card__tag--warn')).toBe(true);
    expect(host.querySelector('[data-tag="frontend"]')!.tagName).toBe('BUTTON');
    expect(host.querySelector('.pill-brand, .badge')).toBeNull();
    app.unmount(); host.remove();
  });

  it('renders the forward transition as the ink button and the rest as ghosts', async () => {
    const { host, app } = await mountCard(task({ status: 'waiting', session_id: 's' }));
    const btns = Array.from(host.querySelectorAll<HTMLButtonElement>('.task-card__actions .btn'));
    expect(btns.map((b) => b.dataset.action)).toEqual(['resume', 'test', 'done']);
    expect(btns.filter((b) => !b.classList.contains('ghost')).map((b) => b.dataset.action)).toEqual(['done']);
    app.unmount(); host.remove();
  });

  it('pulses the running state and shows the rank pill on backlog', async () => {
    const { host, app } = await mountCard(task({ status: 'in_progress' }), 3);
    const state = host.querySelector('[data-role="state"]')!;
    expect(state.classList.contains('pill-run')).toBe(true);
    expect(state.classList.contains('pulse')).toBe(true);
    expect(host.querySelector('.task-card__rank')!.textContent).toBe('#3');
    app.unmount(); host.remove();
  });
});
