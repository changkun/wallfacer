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

describe('github store repo selection', () => {
  it('lists repos', async () => {
    apiMock.mockResolvedValueOnce({ repos: [{ owner: 'latere', name: 'wallfacer', full_name: 'latere/wallfacer', default_branch: 'main', private: true }] });
    const store = useGithubStore();
    await store.fetchRepos();
    expect(store.repos).toHaveLength(1);
    expect(store.repos[0].full_name).toBe('latere/wallfacer');
  });

  it('selecting a repo sets selection and loads the active tab', async () => {
    const store = useGithubStore();
    apiMock
      .mockResolvedValueOnce({ owner: 'latere', name: 'wallfacer', full_name: 'latere/wallfacer', default_branch: 'main', private: true }) // select
      .mockResolvedValueOnce({ pulls: [{ number: 42, title: 'T', state: 'open', author: 'o' }] }); // refresh -> pulls
    await store.selectRepo('latere/wallfacer');
    expect(store.hasRepo).toBe(true);
    expect(store.selectedRepo?.full_name).toBe('latere/wallfacer');
    expect(store.pulls).toHaveLength(1);
    expect(apiMock).toHaveBeenNthCalledWith(2, 'GET', '/api/github/pulls?repo=latere%2Fwallfacer&state=open');
  });
});

describe('github store read surface', () => {
  it('switching to issues tab loads issues', async () => {
    const store = useGithubStore();
    apiMock.mockResolvedValueOnce({ owner: 'l', name: 'w', full_name: 'l/w', default_branch: 'main', private: false });
    apiMock.mockResolvedValueOnce({ pulls: [] });
    await store.selectRepo('l/w');

    apiMock.mockResolvedValueOnce({ issues: [{ number: 7, title: 'I', state: 'open', author: 'a' }] });
    await store.setTab('issues');
    expect(store.tab).toBe('issues');
    expect(store.issues).toHaveLength(1);
    expect(apiMock).toHaveBeenLastCalledWith('GET', '/api/github/issues?repo=l%2Fw&state=open');
  });

  it('state filter re-queries the active tab', async () => {
    const store = useGithubStore();
    apiMock.mockResolvedValueOnce({ owner: 'l', name: 'w', full_name: 'l/w', default_branch: 'main', private: false });
    apiMock.mockResolvedValueOnce({ pulls: [] });
    await store.selectRepo('l/w');

    apiMock.mockResolvedValueOnce({ pulls: [{ number: 1, title: 'closed pr', state: 'closed', author: 'a' }] });
    await store.setStateFilter('closed');
    expect(store.stateFilter).toBe('closed');
    expect(apiMock).toHaveBeenLastCalledWith('GET', '/api/github/pulls?repo=l%2Fw&state=closed');
  });

  it('opens a PR detail', async () => {
    const store = useGithubStore();
    apiMock.mockResolvedValueOnce({ owner: 'l', name: 'w', full_name: 'l/w', default_branch: 'main', private: false });
    apiMock.mockResolvedValueOnce({ pulls: [] });
    await store.selectRepo('l/w');

    apiMock.mockResolvedValueOnce({ number: 42, title: 'T', state: 'open', author: 'o', comments: [] });
    await store.openDetail(42);
    expect(store.detail?.number).toBe(42);
    expect(apiMock).toHaveBeenLastCalledWith('GET', '/api/github/pulls/42?repo=l%2Fw');
  });
});

describe('github store write surface', () => {
  async function selectAndOpen(store: ReturnType<typeof useGithubStore>) {
    apiMock.mockResolvedValueOnce({ owner: 'l', name: 'w', full_name: 'l/w', default_branch: 'main', private: false });
    apiMock.mockResolvedValueOnce({ pulls: [] });
    await store.selectRepo('l/w');
    apiMock.mockResolvedValueOnce({ number: 42, title: 'T', state: 'open', author: 'o', comments: [] });
    await store.openDetail(42);
  }

  it('posts a comment then refreshes the detail thread', async () => {
    const store = useGithubStore();
    await selectAndOpen(store);

    apiMock.mockResolvedValueOnce({}); // POST comment
    apiMock.mockResolvedValueOnce({ number: 42, title: 'T', state: 'open', author: 'o', comments: [{ author: 'me', body: 'hi' }] }); // re-open
    await store.comment('hi');

    expect(apiMock).toHaveBeenNthCalledWith(4, 'POST', '/api/github/comments', { repo: 'l/w', number: 42, body: 'hi' });
    expect(store.detail?.comments).toHaveLength(1);
  });

  it('does not post an empty comment', async () => {
    const store = useGithubStore();
    await selectAndOpen(store);
    const before = apiMock.mock.calls.length;
    await store.comment('   ');
    expect(apiMock.mock.calls.length).toBe(before);
  });

  it('creates a pull request for the selected repo', async () => {
    const store = useGithubStore();
    apiMock.mockResolvedValueOnce({ owner: 'l', name: 'w', full_name: 'l/w', default_branch: 'main', private: false });
    apiMock.mockResolvedValueOnce({ pulls: [] });
    await store.selectRepo('l/w');

    apiMock.mockResolvedValueOnce({ number: 7, title: 'T', state: 'open', author: 'me', html_url: 'u' });
    const pr = await store.createPull({ base: 'main', head: 'feature', title: 'T', body: 'B' });
    expect(pr?.number).toBe(7);
    expect(apiMock).toHaveBeenLastCalledWith('POST', '/api/github/pulls', {
      repo: 'l/w', base: 'main', head: 'feature', title: 'T', body: 'B',
    });
  });
});

describe('github store disconnect', () => {
  it('clears selection and reloads status', async () => {
    const store = useGithubStore();
    apiMock.mockResolvedValueOnce({ owner: 'l', name: 'w', full_name: 'l/w', default_branch: 'main', private: false });
    apiMock.mockResolvedValueOnce({ pulls: [] });
    await store.selectRepo('l/w');

    apiMock.mockResolvedValueOnce(undefined); // disconnect POST
    apiMock.mockResolvedValueOnce({ available: true, connected: false, can_connect: false }); // fetchStatus
    await store.disconnect();
    expect(store.selectedRepo).toBeNull();
    expect(store.connected).toBe(false);
  });
});
