// The GitHub store holds only the connection state now: status (mirrored from
// /api/config and the auth/status endpoint) plus connect/disconnect. The
// standalone repo/PR/issue browse was removed (task-centric redesign); PRs are
// surfaced on tasks/specs, which own their own PR state.
import { defineStore } from 'pinia';
import { ref, computed } from 'vue';

import { api } from '../api/client';

export interface GithubStatus {
  available: boolean;
  connected: boolean;
  login?: string;
  account?: string;
  permissions?: string[];
  expires_at?: string;
  can_connect: boolean;
}

const emptyStatus: GithubStatus = { available: false, connected: false, can_connect: false };

export const useGithubStore = defineStore('github', () => {
  const status = ref<GithubStatus>({ ...emptyStatus });
  const error = ref<string | null>(null);

  const connected = computed(() => status.value.connected);

  function setError(e: unknown) {
    error.value = e instanceof Error ? e.message : String(e);
  }

  async function fetchStatus(): Promise<void> {
    try {
      status.value = await api<GithubStatus>('GET', '/api/github/auth/status');
    } catch (e) {
      status.value = { ...emptyStatus };
      setError(e);
    }
  }

  // connect starts the brokered install + grant flow. The server returns the
  // ../auth install-start URL; navigating there runs the GitHub install and the
  // ../auth callback captures the user token. On return, fetchStatus resolves it
  // and flips to connected.
  async function connect(): Promise<void> {
    error.value = null;
    try {
      const returnTo = encodeURIComponent(window.location.href);
      const resp = await api<{ install_url?: string }>(
        'POST', `/api/github/auth/connect?return_to=${returnTo}`);
      if (resp?.install_url) {
        window.location.href = resp.install_url;
        return;
      }
      await fetchStatus();
    } catch (e) {
      setError(e);
    }
  }

  async function disconnect(): Promise<void> {
    error.value = null;
    try {
      await api('POST', '/api/github/auth/disconnect');
    } catch (e) {
      setError(e);
    }
    await fetchStatus();
  }

  return { status, error, connected, fetchStatus, connect, disconnect };
});
