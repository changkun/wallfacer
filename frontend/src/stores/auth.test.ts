import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, setActivePinia } from 'pinia';

import { useAuthStore } from './auth';

// respond stubs fetch with one response of the given status and JSON body,
// through the real API client, so the error the session code sees is the one
// the client throws.
function respond(status: number, statusText: string, body: unknown) {
  const text = body === undefined ? '' : JSON.stringify(body);
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({
    ok: status >= 200 && status < 300,
    status,
    statusText,
    headers: { get: () => 'application/json' },
    text: () => Promise.resolve(text),
  } as unknown as Response)));
}

beforeEach(() => setActivePinia(createPinia()));
afterEach(() => vi.unstubAllGlobals());

describe('auth store session resolution', () => {
  it('a 401 from /api/me is a signed-out session, not an error', async () => {
    respond(401, 'Unauthorized', { error: { code: 'unauthorized', message: 'Sign in to continue.' } });
    const auth = useAuthStore();
    await auth.fetchMe();
    expect(auth.me).toBeNull();
    expect(auth.error).toBeNull();
    expect(auth.loaded).toBe(true);
  });

  it('a 5xx from /api/me is recorded as an error', async () => {
    respond(502, 'Bad Gateway', {
      error: { code: 'account_unavailable', message: 'The signed-in account could not be loaded from the sign-in service. Try again in a moment.' },
    });
    const auth = useAuthStore();
    await auth.fetchMe();
    expect(auth.me).toBeNull();
    expect(auth.error).toBe('The signed-in account could not be loaded from the sign-in service. Try again in a moment.');
  });

  it('a principal from /api/me is the session', async () => {
    respond(200, 'OK', { principal_id: 'u-1', sub: 'u-1', email: 'a@b.com' });
    const auth = useAuthStore();
    await auth.fetchMe();
    expect(auth.me?.principal_id).toBe('u-1');
    expect(auth.error).toBeNull();
  });
});
