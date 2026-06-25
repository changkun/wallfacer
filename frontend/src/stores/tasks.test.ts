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
