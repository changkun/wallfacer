import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, setActivePinia } from 'pinia';

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
vi.mock('../api/client', () => ({
  api: apiMock,
}));

import { useTaskStore } from './tasks';

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
