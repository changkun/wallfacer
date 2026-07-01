import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
// Keep the real ApiError so the composable's `instanceof ApiError` 503 check works.
vi.mock('../api/client', async (orig) => {
  const actual = await orig<typeof import('../api/client')>();
  return { ...actual, api: apiMock };
});

import { ApiError } from '../api/client';
import { useDeviceSignIn } from './useDeviceSignIn';

const startBody = {
  user_code: 'ABCD-1234',
  verification_uri: 'https://auth.latere.ai/device',
  verification_uri_complete: 'https://auth.latere.ai/device?user_code=ABCD-1234',
  expires_in: 600,
};

beforeEach(() => {
  vi.useFakeTimers();
  apiMock.mockReset();
});
afterEach(() => {
  vi.useRealTimers();
});

describe('useDeviceSignIn', () => {
  it('starts the flow and exposes the code + verification url', async () => {
    apiMock.mockResolvedValueOnce(startBody);
    const d = useDeviceSignIn();

    const started = await d.start();

    expect(started).toBe(true);
    expect(d.status.value).toBe('pending');
    expect(d.userCode.value).toBe('ABCD-1234');
    expect(d.verificationUriComplete.value).toContain('user_code=ABCD-1234');
    d.reset();
  });

  it('reports not-started on 503 so the caller can fall back', async () => {
    apiMock.mockRejectedValueOnce(new ApiError(503, null, 'device-code auth not configured'));
    const d = useDeviceSignIn();

    const started = await d.start();

    expect(started).toBe(false);
    expect(d.status.value).toBe('idle');
  });

  it('loginOrFallback invokes the fallback when device sign-in is unavailable', async () => {
    apiMock.mockRejectedValueOnce(new ApiError(503, null, 'unavailable'));
    const d = useDeviceSignIn();
    const fallback = vi.fn();

    await d.loginOrFallback(fallback);

    expect(fallback).toHaveBeenCalledTimes(1);
    expect(d.status.value).toBe('idle');
  });

  it('loginOrFallback does not fall back when the device flow starts', async () => {
    apiMock.mockResolvedValueOnce(startBody);
    const d = useDeviceSignIn();
    const fallback = vi.fn();

    await d.loginOrFallback(fallback);

    expect(fallback).not.toHaveBeenCalled();
    expect(d.status.value).toBe('pending');
    d.reset();
  });

  it('transitions to done when the poll completes', async () => {
    apiMock.mockResolvedValueOnce(startBody);
    const d = useDeviceSignIn();
    await d.start();

    apiMock.mockResolvedValueOnce({ status: 'done' });
    await d.poll();

    expect(d.status.value).toBe('done');
  });

  it('surfaces a denied poll as a terminal error', async () => {
    apiMock.mockResolvedValueOnce(startBody);
    const d = useDeviceSignIn();
    await d.start();

    apiMock.mockResolvedValueOnce({ status: 'denied', error: 'access_denied' });
    await d.poll();

    expect(d.status.value).toBe('error');
    expect(d.error.value).toBe('denied');
  });

  it('cancel posts to the cancel endpoint and resets', async () => {
    apiMock.mockResolvedValueOnce(startBody);
    const d = useDeviceSignIn();
    await d.start();

    apiMock.mockResolvedValueOnce(null);
    await d.cancel();

    expect(apiMock).toHaveBeenLastCalledWith('POST', '/api/auth/device/cancel');
    expect(d.status.value).toBe('idle');
  });
});
