import { describe, it, expect, afterEach, vi } from 'vitest';
import { ApiError as SessionApiError } from 'latere-ui';
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

  it('surfaces the message and the code of an error envelope', async () => {
    const message = 'Sign-in by browser redirect is off because Wallfacer is not running on its configured port.';
    vi.stubGlobal('fetch', mockFetch(503, 'Service Unavailable', JSON.stringify({
      error: {
        code: 'redirect_sign_in_unavailable',
        message,
        details: { redirect_url: 'http://localhost:8080/callback', bound_port: 53211 },
      },
    }), 'application/json'));
    const err = await api('POST', '/api/me/switch-org', { org_id: '' }).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 503, message, code: 'redirect_sign_in_unavailable' });
    // The developer detail stays on the parsed body.
    expect((err as ApiError).body).toMatchObject({ error: { details: { bound_port: 53211 } } });
  });

  it('keeps the code of an envelope that carries no message', async () => {
    vi.stubGlobal('fetch', mockFetch(503, 'Service Unavailable',
      JSON.stringify({ error: { code: 'redirect_sign_in_unavailable' } }), 'application/json'));
    await expect(api('GET', '/api/x')).rejects.toMatchObject({
      message: 'Service Unavailable',
      code: 'redirect_sign_in_unavailable',
    });
  });

  it('leaves the code empty for the string error form and for plain text', async () => {
    vi.stubGlobal('fetch', mockFetch(409, 'Conflict',
      JSON.stringify({ error: 'thread locked' }), 'application/json'));
    await expect(api('POST', '/api/x')).rejects.toMatchObject({ message: 'thread locked', code: '' });

    vi.stubGlobal('fetch', mockFetch(503, 'Service Unavailable', 'auth not configured', 'text/plain'));
    await expect(api('GET', '/api/x')).rejects.toMatchObject({ message: 'auth not configured', code: '' });
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

  it('throws an error the shared session code recognizes, keeping the code', async () => {
    vi.stubGlobal('fetch', mockFetch(401, 'Unauthorized', JSON.stringify({
      error: { code: 'unauthorized', message: 'Sign in to continue.' },
    }), 'application/json'));
    const err = await api('GET', '/api/me').catch((e: unknown) => e);
    expect(err).toBeInstanceOf(SessionApiError);
    expect(err).toMatchObject({ status: 401, message: 'Sign in to continue.', code: 'unauthorized' });
  });
});
