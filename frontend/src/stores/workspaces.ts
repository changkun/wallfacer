// The workspaces store backs the first-class workspace model: a workspace has a
// stable id, a name, and a mutable set of folder paths. Identity is decoupled
// from membership, so renaming or re-pointing folders never loses history.
//
// Endpoints (see internal/handler/workspaces.go):
//   GET    /api/workspaces            -> { workspaces, active_id }
//   POST   /api/workspaces            -> WorkspaceDTO (201, created, not active)
//   PUT    /api/workspaces/{id}       -> WorkspaceDTO (rename / replace folders)
//   DELETE /api/workspaces/{id}       -> 204 (409 if active)
//   POST   /api/workspaces/{id}/activate -> full /api/config payload, switches board
//
// The legacy PUT /api/workspaces { workspaces: string[] } path-based switch is
// untouched and still served; WorkspacePicker's wizard now prefers create+activate.
import { defineStore } from 'pinia';
import { ref, computed } from 'vue';

import { api } from '../api/client';
import { useTaskStore } from './tasks';
import { useUiStore } from './ui';
import type { ServerConfig, Workspace } from '../api/types';

export const useWorkspacesStore = defineStore('workspaces', () => {
  const workspaces = ref<Workspace[]>([]);
  const loading = ref(false);
  const error = ref<string | null>(null);

  // The active workspace is whatever the server reports in /api/config
  // (workspace_id) — the single source of truth. Deriving it (rather than
  // caching a per-fetch `active` flag) keeps the sidebar, settings, and picker
  // in agreement no matter which path performed the switch. Components should
  // determine "is this workspace active" via isActive(), not the DTO's stale
  // `active` field.
  const activeId = computed(() => useTaskStore().config?.workspace_id ?? '');
  function isActive(id: string): boolean {
    return id !== '' && id === activeId.value;
  }

  const active = computed(() =>
    workspaces.value.find(w => w.id === activeId.value) ?? null,
  );

  function setError(e: unknown) {
    error.value = e instanceof Error ? e.message : String(e);
  }

  // list loads the full workspace registry and the active id.
  async function list(): Promise<void> {
    loading.value = true;
    error.value = null;
    try {
      const resp = await api<{ workspaces: Workspace[]; active_id: string }>(
        'GET', '/api/workspaces');
      workspaces.value = resp.workspaces ?? [];
    } catch (e) {
      setError(e);
    } finally {
      loading.value = false;
    }
  }

  // create registers a new workspace. It does NOT activate it; the returned
  // DTO carries the new id for callers that want to activate next.
  async function create(name: string, folders: string[]): Promise<Workspace> {
    error.value = null;
    const ws = await api<Workspace>('POST', '/api/workspaces', { name, folders });
    workspaces.value.push(ws);
    return ws;
  }

  // update renames and/or replaces the folders of a workspace in place.
  // History is preserved server-side; the returned DTO replaces the local copy.
  async function update(
    id: string,
    patch: {
      name?: string;
      folders?: string[];
      max_parallel?: number | null;
      max_test_parallel?: number | null;
    },
  ): Promise<Workspace> {
    error.value = null;
    const ws = await api<Workspace>('PUT', `/api/workspaces/${id}`, patch);
    const idx = workspaces.value.findIndex(w => w.id === id);
    if (idx >= 0) workspaces.value[idx] = ws;
    return ws;
  }

  // remove permanently deletes a workspace and wipes its data. The active
  // workspace may be deleted: the server auto-switches the board (to the next
  // workspace or the empty state) and returns the resulting config, which we
  // apply so the sidebar + board reflect the deletion in one round-trip.
  async function remove(id: string): Promise<void> {
    error.value = null;
    const config = await api<ServerConfig>('DELETE', `/api/workspaces/${id}`);
    workspaces.value = workspaces.value.filter(w => w.id !== id);
    const tasks = useTaskStore();
    tasks.config = config;
    await tasks.fetchTasks();
  }

  // activate switches the board to a workspace. The endpoint returns the full
  // config payload; we hand it to the tasks store (the config-bearing store) so
  // the board reflects the switch without a second round-trip, then reload the
  // task list for the new active workspace.
  async function activate(id: string): Promise<void> {
    error.value = null;
    // Raise the blocking overlay for the whole switch so the UI never paints
    // the new active state over stale old content; lower it in finally even on
    // failure.
    const ui = useUiStore();
    ui.switchingWorkspace = true;
    try {
      const config = await api<ServerConfig>('POST', `/api/workspaces/${id}/activate`);
      const tasks = useTaskStore();
      // Writing config flips activeId (derived from config.workspace_id) and the
      // per-row active state reactively; no manual bookkeeping needed.
      tasks.config = config;
      await tasks.fetchTasks();
    } finally {
      ui.switchingWorkspace = false;
    }
  }

  return {
    workspaces, activeId, isActive, loading, error, active,
    list, create, update, remove, activate,
  };
});
