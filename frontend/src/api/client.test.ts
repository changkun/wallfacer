import { describe, it, expect, afterEach } from 'vitest';
import { authHeaders, withAuthToken, getServerApiKey } from './client';

declare global {
  interface Window {
    __WALLFACER__?: { serverApiKey?: string } & Record<string, unknown>;
  }
}

function setKey(key: string | undefined) {
  // @ts-expect-error test shim for the injected boot global
  window.__WALLFACER__ = key === undefined ? undefined : { serverApiKey: key };
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
