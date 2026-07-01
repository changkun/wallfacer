import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, setActivePinia } from 'pinia';

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
vi.mock('../api/client', () => ({ api: apiMock }));

import { useGithubPrStore } from './githubPr';

beforeEach(() => {
  setActivePinia(createPinia());
  apiMock.mockReset();
  apiMock.mockResolvedValue({});
});

describe('githubPr store', () => {
  it('fetches and caches a task PR', async () => {
    apiMock.mockResolvedValueOnce({ pull_request: { number: 42, title: 'T', state: 'open', author: 'o' } });
    const store = useGithubPrStore();
    await store.fetchTaskPR('task-1');
    expect(store.prFor('task-1')?.number).toBe(42);
    expect(apiMock).toHaveBeenCalledWith('GET', '/api/tasks/task-1/pr');
  });

  it('caches "no PR" as null', async () => {
    apiMock.mockResolvedValueOnce({ pull_request: null });
    const store = useGithubPrStore();
    await store.fetchTaskPR('task-2');
    expect(store.prFor('task-2')).toBeNull();
  });

  it('a task with no github branch resolves quietly to null', async () => {
    apiMock.mockRejectedValueOnce(new Error('no github branch'));
    const store = useGithubPrStore();
    await store.fetchTaskPR('task-3');
    expect(store.prFor('task-3')).toBeNull();
  });

  it('creates a PR and caches it', async () => {
    apiMock.mockResolvedValueOnce({ number: 7, title: 'New', state: 'open', author: 'me', html_url: 'u' });
    const store = useGithubPrStore();
    const pr = await store.createTaskPR('task-4', { title: 'New' });
    expect(pr?.number).toBe(7);
    expect(store.prFor('task-4')?.number).toBe(7);
    expect(apiMock).toHaveBeenCalledWith('POST', '/api/tasks/task-4/pr', { title: 'New' });
  });

  it('posts a comment', async () => {
    apiMock.mockResolvedValueOnce({});
    const store = useGithubPrStore();
    const ok = await store.commentTaskPR('task-5', 'nice');
    expect(ok).toBe(true);
    expect(apiMock).toHaveBeenCalledWith('POST', '/api/tasks/task-5/pr/comment', { body: 'nice' });
  });

  it('does not post an empty comment', async () => {
    const store = useGithubPrStore();
    const ok = await store.commentTaskPR('task-6', '  ');
    expect(ok).toBe(false);
    expect(apiMock).not.toHaveBeenCalled();
  });
});
