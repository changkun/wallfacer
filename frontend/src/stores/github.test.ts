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
    apiMock.mockResolvedValueOnce({
      available: true, connected: true, login: 'octocat', can_connect: false,
      manage_url: 'https://auth.latere.ai/me',
    });
    const store = useGithubStore();
    await store.fetchStatus();
    expect(store.connected).toBe(true);
    expect(store.status.login).toBe('octocat');
    expect(store.manageUrl).toBe('https://auth.latere.ai/me');
  });

  it('falls back to a default manage url', async () => {
    apiMock.mockResolvedValueOnce({ available: true, connected: false, can_connect: false });
    const store = useGithubStore();
    await store.fetchStatus();
    expect(store.manageUrl).toBe('https://auth.latere.ai/me');
  });

  it('resets to disconnected on status error', async () => {
    apiMock.mockRejectedValueOnce(new Error('boom'));
    const store = useGithubStore();
    await store.fetchStatus();
    expect(store.connected).toBe(false);
    expect(store.error).toBe('boom');
  });
});
