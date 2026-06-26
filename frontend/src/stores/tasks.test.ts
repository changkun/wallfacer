import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, setActivePinia } from 'pinia';

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
vi.mock('../api/client', () => ({
  api: apiMock,
}));

import { useTaskStore } from './tasks';
import { dependencyBadge } from '../lib/cardBadges';
import type { Task } from '../api/types';

beforeEach(() => {
  setActivePinia(createPinia());
  apiMock.mockReset();
  apiMock.mockResolvedValue({});
});

describe('tasks store create payloads', () => {
  it('omits timeout for a single task when unset', async () => {
    const store = useTaskStore();

    await store.createTask('ship it');

    expect(apiMock).toHaveBeenCalledWith('POST', '/api/tasks', { prompt: 'ship it' });
  });

  it('keeps an explicit single-task timeout', async () => {
    const store = useTaskStore();

    await store.createTask('ship it', { timeout: 45 });

    expect(apiMock).toHaveBeenCalledWith('POST', '/api/tasks', { prompt: 'ship it', timeout: 45 });
  });

  it('omits timeout for batch tasks when unset', async () => {
    const store = useTaskStore();

    await store.batchCreateTasks(['one', 'two'], { flow: 'implement' });

    expect(apiMock).toHaveBeenCalledWith('POST', '/api/tasks/batch', {
      tasks: [
        { prompt: 'one', flow: 'implement' },
        { prompt: 'two', flow: 'implement' },
      ],
    });
  });

  it('keeps an explicit batch timeout', async () => {
    const store = useTaskStore();

    await store.batchCreateTasks(['one', 'two'], { timeout: 45 });

    expect(apiMock).toHaveBeenCalledWith('POST', '/api/tasks/batch', {
      tasks: [
        { prompt: 'one', timeout: 45 },
        { prompt: 'two', timeout: 45 },
      ],
    });
  });
});

describe('tasks store tasksById', () => {
  it('indexes tasks by id and rebuilds on change', () => {
    const store = useTaskStore();
    store.setTasks([
      { id: 'a' } as Task,
      { id: 'b' } as Task,
    ]);
    expect(store.tasksById.get('a')?.id).toBe('a');
    expect(store.tasksById.get('b')?.id).toBe('b');

    store.setTasks([{ id: 'c' } as Task]);
    expect(store.tasksById.has('a')).toBe(false);
    expect(store.tasksById.get('c')?.id).toBe('c');
  });

  it('resolves a backlog blocked badge via the shared map', () => {
    const store = useTaskStore();
    const dep = { id: 'dep', title: 'Build deps', status: 'in_progress' } as Task;
    const card = { id: 'card', status: 'backlog', depends_on: ['dep'] } as Task;
    store.setTasks([dep, card]);

    const badge = dependencyBadge(card, store.tasksById);
    expect(badge?.kind).toBe('blocked');
    expect(badge?.blocking).toBe('Build deps');
  });
});

describe('tasks store done column ordering', () => {
  it('orders done tasks by last updated time, most recent first', () => {
    const store = useTaskStore();
    store.setTasks([
      { id: 'old', status: 'done', updated_at: '2026-06-01T00:00:00Z' } as Task,
      { id: 'new', status: 'done', updated_at: '2026-06-26T00:00:00Z' } as Task,
      { id: 'mid', status: 'cancelled', updated_at: '2026-06-15T00:00:00Z' } as Task,
    ]);

    expect(store.done.map(t => t.id)).toEqual(['new', 'mid', 'old']);
  });

  it('falls back to created_at when updated_at is missing', () => {
    const store = useTaskStore();
    store.setTasks([
      { id: 'a', status: 'done', created_at: '2026-06-01T00:00:00Z' } as Task,
      { id: 'b', status: 'done', created_at: '2026-06-10T00:00:00Z' } as Task,
    ]);

    expect(store.done.map(t => t.id)).toEqual(['b', 'a']);
  });
});
