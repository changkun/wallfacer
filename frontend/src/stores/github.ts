// The GitHub store holds the connection state only, and it is read-only from
// wallfacer's side: connecting/disconnecting GitHub happens centrally at
// auth.latere.ai (the connectors hub). wallfacer borrows the connection of the
// signed-in latere.ai account -- fetchStatus resolves it via the broker.
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
  // manage_url is the auth.latere.ai account page where GitHub is connected.
  manage_url?: string;
}

const emptyStatus: GithubStatus = { available: false, connected: false, can_connect: false };

export const useGithubStore = defineStore('github', () => {
  const status = ref<GithubStatus>({ ...emptyStatus });
  const error = ref<string | null>(null);

  const connected = computed(() => status.value.connected);
  const manageUrl = computed(() => status.value.manage_url || 'https://auth.latere.ai/me');

  async function fetchStatus(): Promise<void> {
    try {
      status.value = await api<GithubStatus>('GET', '/api/github/auth/status');
    } catch (e) {
      status.value = { ...emptyStatus };
      error.value = e instanceof Error ? e.message : String(e);
    }
  }

  return { status, error, connected, manageUrl, fetchStatus };
});
