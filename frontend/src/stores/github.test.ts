import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, setActivePinia } from 'pinia';

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
vi.mock('../api/client', () => ({ api: apiMock }));

import { useGithubStore } from './github';

beforeEach(() => {
  setActivePinia(createPinia());
  apiMock.mockReset();
  apiMock.mockResolvedValue({});
});

describe('github store status', () => {
  it('loads connection status', async () => {
    apiMock.mockResolvedValueOnce({ available: true, connected: true, login: 'octocat', can_connect: false });
    const store = useGithubStore();
    await store.fetchStatus();
    expect(store.connected).toBe(true);
    expect(store.status.login).toBe('octocat');
  });

  it('resets to disconnected on status error', async () => {
    apiMock.mockRejectedValueOnce(new Error('boom'));
    const store = useGithubStore();
    await store.fetchStatus();
    expect(store.connected).toBe(false);
    expect(store.error).toBe('boom');
  });
});

describe('github store disconnect', () => {
  it('clears and reloads status', async () => {
    const store = useGithubStore();
    apiMock.mockResolvedValueOnce(undefined); // disconnect POST
    apiMock.mockResolvedValueOnce({ available: true, connected: false, can_connect: false }); // fetchStatus
    await store.disconnect();
    expect(store.connected).toBe(false);
    expect(apiMock).toHaveBeenNthCalledWith(1, 'POST', '/api/github/auth/disconnect');
  });
});
