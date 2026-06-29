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
import type { ServerConfig, Workspace } from '../api/types';

export const useWorkspacesStore = defineStore('workspaces', () => {
  const workspaces = ref<Workspace[]>([]);
  const activeId = ref('');
  const loading = ref(false);
  const error = ref<string | null>(null);

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
      activeId.value = resp.active_id ?? '';
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
    patch: { name?: string; folders?: string[] },
  ): Promise<Workspace> {
    error.value = null;
    const ws = await api<Workspace>('PUT', `/api/workspaces/${id}`, patch);
    const idx = workspaces.value.findIndex(w => w.id === id);
    if (idx >= 0) workspaces.value[idx] = ws;
    return ws;
  }

  // remove deletes a workspace. The server returns 409 for the active one, so
  // callers must switch away first.
  async function remove(id: string): Promise<void> {
    error.value = null;
    await api<void>('DELETE', `/api/workspaces/${id}`);
    workspaces.value = workspaces.value.filter(w => w.id !== id);
  }

  // activate switches the board to a workspace. The endpoint returns the full
  // config payload; we hand it to the tasks store (the config-bearing store) so
  // the board reflects the switch without a second round-trip, then reload the
  // task list for the new active workspace.
  async function activate(id: string): Promise<void> {
    error.value = null;
    const config = await api<ServerConfig>('POST', `/api/workspaces/${id}/activate`);
    const tasks = useTaskStore();
    tasks.config = config;
    activeId.value = config.workspace_id ?? id;
    for (const w of workspaces.value) w.active = w.id === activeId.value;
    await tasks.fetchTasks();
  }

  return {
    workspaces, activeId, loading, error, active,
    list, create, update, remove, activate,
  };
});
