// The GitHub store backs the /api/github/* surface: connection status (mirrored
// from /api/config and the auth/status endpoint), the install-granted repo list
// and current selection, and the PR/issue list + detail state for the /github
// page. Selection lives here (client-side) per the repo-selection design; the
// read endpoints are keyed on the selected repo's "owner/name".
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

export interface GithubRepo {
  owner: string;
  name: string;
  full_name: string;
  default_branch: string;
  private: boolean;
  pushed_at?: string;
  html_url?: string;
}

export interface GithubComment {
  author: string;
  body: string;
  created_at?: string;
  html_url?: string;
}

export interface GithubPull {
  number: number;
  title: string;
  state: string;
  author: string;
  draft?: boolean;
  created_at?: string;
  updated_at?: string;
  html_url?: string;
  body?: string;
  comments?: GithubComment[];
}

export interface GithubIssue {
  number: number;
  title: string;
  state: string;
  author: string;
  labels?: string[];
  created_at?: string;
  updated_at?: string;
  html_url?: string;
  body?: string;
  comments?: GithubComment[];
}

export type GithubTab = 'pulls' | 'issues';
export type GithubState = 'open' | 'closed' | 'all';

const emptyStatus: GithubStatus = { available: false, connected: false, can_connect: false };

export const useGithubStore = defineStore('github', () => {
  const status = ref<GithubStatus>({ ...emptyStatus });
  const repos = ref<GithubRepo[]>([]);
  const selectedRepo = ref<GithubRepo | null>(null);

  const tab = ref<GithubTab>('pulls');
  const stateFilter = ref<GithubState>('open');
  const pulls = ref<GithubPull[]>([]);
  const issues = ref<GithubIssue[]>([]);
  const detail = ref<GithubPull | GithubIssue | null>(null);

  const loading = ref(false);
  const commenting = ref(false);
  const error = ref<string | null>(null);

  const connected = computed(() => status.value.connected);
  const hasRepo = computed(() => selectedRepo.value !== null);

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
  // via the broker and flips to connected.
  async function connect(): Promise<void> {
    error.value = null;
    try {
      // Pass the current page as return_to so the ../auth install flow returns
      // here (loopback returns are allowed by auth) instead of dropping the user
      // on auth.latere.ai/me.
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
    selectedRepo.value = null;
    repos.value = [];
    pulls.value = [];
    issues.value = [];
    detail.value = null;
    await fetchStatus();
  }

  async function fetchRepos(): Promise<void> {
    loading.value = true;
    error.value = null;
    try {
      const resp = await api<{ repos: GithubRepo[] }>('GET', '/api/github/repos');
      repos.value = resp.repos ?? [];
    } catch (e) {
      setError(e);
    } finally {
      loading.value = false;
    }
  }

  async function selectRepo(fullName: string): Promise<void> {
    error.value = null;
    try {
      const resp = await api<GithubRepo>('POST', '/api/github/repo/select', { repo: fullName });
      selectedRepo.value = resp;
      detail.value = null;
      await refresh();
    } catch (e) {
      setError(e);
    }
  }

  function repoQuery(): string {
    return selectedRepo.value ? encodeURIComponent(selectedRepo.value.full_name) : '';
  }

  async function fetchPulls(): Promise<void> {
    if (!selectedRepo.value) return;
    loading.value = true;
    error.value = null;
    try {
      const resp = await api<{ pulls: GithubPull[] }>(
        'GET', `/api/github/pulls?repo=${repoQuery()}&state=${stateFilter.value}`);
      pulls.value = resp.pulls ?? [];
    } catch (e) {
      setError(e);
    } finally {
      loading.value = false;
    }
  }

  async function fetchIssues(): Promise<void> {
    if (!selectedRepo.value) return;
    loading.value = true;
    error.value = null;
    try {
      const resp = await api<{ issues: GithubIssue[] }>(
        'GET', `/api/github/issues?repo=${repoQuery()}&state=${stateFilter.value}`);
      issues.value = resp.issues ?? [];
    } catch (e) {
      setError(e);
    } finally {
      loading.value = false;
    }
  }

  // refresh loads the active tab's list for the selected repo.
  async function refresh(): Promise<void> {
    if (tab.value === 'pulls') {
      await fetchPulls();
    } else {
      await fetchIssues();
    }
  }

  async function openDetail(number: number): Promise<void> {
    if (!selectedRepo.value) return;
    loading.value = true;
    error.value = null;
    try {
      const kind = tab.value === 'pulls' ? 'pulls' : 'issues';
      detail.value = await api<GithubPull | GithubIssue>(
        'GET', `/api/github/${kind}/${number}?repo=${repoQuery()}`);
    } catch (e) {
      setError(e);
    } finally {
      loading.value = false;
    }
  }

  // comment posts a conversation comment on the open PR/issue, then refreshes
  // the detail so the new comment appears in the thread.
  async function comment(body: string): Promise<void> {
    if (!selectedRepo.value || !detail.value || !body.trim()) return;
    commenting.value = true;
    error.value = null;
    try {
      await api('POST', '/api/github/comments', {
        repo: selectedRepo.value.full_name,
        number: detail.value.number,
        body,
      });
      await openDetail(detail.value.number);
    } catch (e) {
      setError(e);
    } finally {
      commenting.value = false;
    }
  }

  // createPull opens a PR from head into base for the selected repo, returning
  // the created (or existing) PR. The branch must already be pushed.
  async function createPull(params: {
    base: string; head: string; title: string; body?: string; draft?: boolean;
  }): Promise<GithubPull | null> {
    if (!selectedRepo.value) return null;
    error.value = null;
    try {
      return await api<GithubPull>('POST', '/api/github/pulls', {
        repo: selectedRepo.value.full_name, ...params,
      });
    } catch (e) {
      setError(e);
      return null;
    }
  }

  async function setTab(next: GithubTab): Promise<void> {
    tab.value = next;
    detail.value = null;
    await refresh();
  }

  async function setStateFilter(next: GithubState): Promise<void> {
    stateFilter.value = next;
    await refresh();
  }

  return {
    status, repos, selectedRepo, tab, stateFilter, pulls, issues, detail,
    loading, commenting, error, connected, hasRepo,
    fetchStatus, connect, disconnect, fetchRepos, selectRepo,
    fetchPulls, fetchIssues, refresh, openDetail, setTab, setStateFilter,
    comment, createPull,
  };
});
