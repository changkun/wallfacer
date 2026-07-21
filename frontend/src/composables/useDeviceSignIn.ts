// Drives the local-mode RFC 8628 device-code sign-in from the account menu:
// start the flow, poll until the user approves on auth.latere.ai, and expose
// reactive state for the modal. On a 503 (device sign-in not wired — e.g. a
// cloud deployment) `start` reports "not started" so the caller falls back to
// the browser /login redirect. Mirrors the start/poll/cancel shape of the
// provider OAuth flow in SettingsTabSandbox.vue.
import { ref } from 'vue';

import { api, ApiError } from '../api/client';
import type { DeviceStartResponse, DevicePollResponse } from '../api/types';

// idle: nothing in flight. starting: /start in flight. pending: waiting for the
// user to approve. done: signed in. error: denied/expired/failed.
export type DeviceSignInStatus = 'idle' | 'starting' | 'pending' | 'done' | 'error';

// pollIntervalMs is the cadence for the local /poll endpoint (cheap, same
// origin); the backend owns the slower upstream device-token polling.
const pollIntervalMs = 2000;

export function useDeviceSignIn() {
  const status = ref<DeviceSignInStatus>('idle');
  const userCode = ref('');
  const verificationUri = ref('');
  const verificationUriComplete = ref('');
  // error holds the terminal reason ('denied' | 'expired' | 'failed') for the
  // modal to localize; empty otherwise.
  const error = ref('');

  let poller: number | undefined;

  function stopPolling() {
    if (poller !== undefined) {
      window.clearInterval(poller);
      poller = undefined;
    }
  }

  function reset() {
    stopPolling();
    status.value = 'idle';
    userCode.value = '';
    verificationUri.value = '';
    verificationUriComplete.value = '';
    error.value = '';
  }

  // start begins the device flow. Returns true when the local flow started (the
  // caller shows the modal) and false when device sign-in is unavailable (503),
  // signalling the caller to fall back to the browser redirect. Other failures
  // reject.
  async function start(): Promise<boolean> {
    reset();
    status.value = 'starting';
    let res: DeviceStartResponse;
    try {
      res = await api<DeviceStartResponse>('POST', '/api/auth/device/start');
    } catch (e) {
      reset();
      if (e instanceof ApiError && e.status === 503) return false;
      throw e;
    }
    userCode.value = res.user_code;
    verificationUri.value = res.verification_uri;
    verificationUriComplete.value = res.verification_uri_complete || res.verification_uri;
    status.value = 'pending';
    poller = window.setInterval(() => void poll(), pollIntervalMs);
    return true;
  }

  // poll checks the in-flight flow once. Exposed so tests can step the flow
  // deterministically without the timer; the interval calls it in production.
  async function poll(): Promise<void> {
    let res: DevicePollResponse;
    try {
      res = await api<DevicePollResponse>('GET', '/api/auth/device/poll');
    } catch {
      // Transient error — keep polling, it may recover.
      return;
    }
    switch (res.status) {
      case 'done':
        stopPolling();
        status.value = 'done';
        break;
      case 'denied':
      case 'expired':
        stopPolling();
        error.value = res.status;
        status.value = 'error';
        break;
      case 'failed':
        // Server-side failure (e.g. the token store could not persist the
        // token). Distinct from user denial; surface it as a generic failure.
        stopPolling();
        error.value = 'failed';
        status.value = 'error';
        break;
      case 'idle':
        // The flow vanished server-side (e.g. a competing start cancelled it).
        stopPolling();
        error.value = 'failed';
        status.value = 'error';
        break;
      // 'pending' — keep waiting.
    }
  }

  // cancel aborts an in-flight flow and clears state.
  async function cancel(): Promise<void> {
    stopPolling();
    try {
      await api('POST', '/api/auth/device/cancel');
    } catch {
      // Best effort — the flow expires server-side regardless.
    }
    reset();
  }

  // loginOrFallback starts the device flow, or invokes fallback (the browser
  // /login redirect) when device sign-in is unavailable or fails to start.
  async function loginOrFallback(fallback: () => void): Promise<void> {
    let started = false;
    try {
      started = await start();
    } catch {
      started = false;
    }
    if (!started) fallback();
  }

  return {
    status,
    userCode,
    verificationUri,
    verificationUriComplete,
    error,
    start,
    poll,
    cancel,
    reset,
    loginOrFallback,
  };
}
