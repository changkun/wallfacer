import { describe, it, expect, afterEach, vi } from 'vitest';
import { api, ApiError, authHeaders, withAuthToken, getServerApiKey } from './client';

function setKey(key: string | undefined) {
  if (key === undefined) {
    (window as { __WALLFACER__?: unknown }).__WALLFACER__ = undefined;
  } else {
    (window as { __WALLFACER__?: { serverApiKey: string } }).__WALLFACER__ = { serverApiKey: key };
  }
}

describe('client auth helpers', () => {
  afterEach(() => setKey(undefined));

  it('getServerApiKey returns the injected key or empty string', () => {
    expect(getServerApiKey()).toBe('');
    setKey('k1');
    expect(getServerApiKey()).toBe('k1');
  });

  it('authHeaders returns a Bearer header only when a key is present', () => {
    expect(authHeaders()).toEqual({});
    setKey('k2');
    expect(authHeaders()).toEqual({ Authorization: 'Bearer k2' });
  });

  it('withAuthToken appends token with the correct separator', () => {
    expect(withAuthToken('/api/x')).toBe('/api/x'); // no key → unchanged
    setKey('k3');
    expect(withAuthToken('/api/x')).toBe('/api/x?token=k3');
    // URL already has a query string → use & (e.g. terminal WS url).
    expect(withAuthToken('/api/x?a=1')).toBe('/api/x?a=1&token=k3');
  });

  it('withAuthToken url-encodes the key', () => {
    setKey('a/b c');
    expect(withAuthToken('/api/x')).toBe('/api/x?token=a%2Fb%20c');
  });
});

function mockFetch(status: number, statusText: string, body: string, contentType: string) {
  const res = {
    ok: status >= 200 && status < 300,
    status,
    statusText,
    headers: { get: () => contentType },
    text: () => Promise.resolve(body),
  };
  return vi.fn(() => Promise.resolve(res as unknown as Response));
}

describe('api error messages', () => {
  afterEach(() => vi.restoreAllMocks());

  it('surfaces a plain-text error body (http.Error) instead of the status text', async () => {
    vi.stubGlobal('fetch', mockFetch(409, 'Conflict',
      'specs/local/x.md: cancel the dispatched task before archiving', 'text/plain'));
    await expect(api('POST', '/api/specs/transition', { action: 'archive' }))
      .rejects.toMatchObject({
        status: 409,
        message: 'specs/local/x.md: cancel the dispatched task before archiving',
      });
  });

  it('prefers a JSON message field', async () => {
    vi.stubGlobal('fetch', mockFetch(409, 'Conflict',
      JSON.stringify({ message: 'task is busy' }), 'application/json'));
    await expect(api('POST', '/api/x')).rejects.toMatchObject({ message: 'task is busy' });
  });

  it('falls back to a JSON error field', async () => {
    vi.stubGlobal('fetch', mockFetch(409, 'Conflict',
      JSON.stringify({ error: 'thread locked' }), 'application/json'));
    await expect(api('POST', '/api/x')).rejects.toMatchObject({ message: 'thread locked' });
  });

  it('falls back to the status text when the body is empty', async () => {
    vi.stubGlobal('fetch', mockFetch(500, 'Internal Server Error', '', 'text/plain'));
    await expect(api('GET', '/api/x')).rejects.toMatchObject({ message: 'Internal Server Error' });
  });

  it('throws ApiError carrying the parsed body', async () => {
    vi.stubGlobal('fetch', mockFetch(404, 'Not Found',
      JSON.stringify({ error: 'missing' }), 'application/json'));
    await expect(api('GET', '/api/x')).rejects.toBeInstanceOf(ApiError);
  });
});
