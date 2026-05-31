import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import { api } from '../api/client';
import { useUiStore } from './ui';
import type { Task, ServerConfig } from '../api/types';

export const useTaskStore = defineStore('tasks', () => {
  const tasks = ref<Task[]>([]);
  const config = ref<ServerConfig | null>(null);
  const loading = ref(true);
  const filterQuery = ref('');
  const ui = useUiStore();

  function matchesFilter(t: Task): boolean {
    const q = filterQuery.value;
    if (!q) return true;
    return (t.title || '').toLowerCase().includes(q)
      || t.prompt.toLowerCase().includes(q)
      || t.id.startsWith(q)
      || t.tags?.some(tag => tag.toLowerCase().includes(q))
      || false;
  }

  const backlog = computed(() =>
    tasks.value
      .filter(t => t.status === 'backlog' && !t.archived && matchesFilter(t))
      .sort((a, b) => a.position - b.position),
  );
  const inProgress = computed(() =>
    tasks.value.filter(t =>
      (t.status === 'in_progress' || t.status === 'committing') && !t.archived && matchesFilter(t),
    ),
  );
  const waiting = computed(() =>
    tasks.value.filter(t =>
      (t.status === 'waiting' || t.status === 'failed') && !t.archived && matchesFilter(t),
    ),
  );
  const done = computed(() =>
    tasks.value.filter(t =>
      (t.status === 'done' || t.status === 'cancelled')
      && (ui.showArchived || !t.archived)
      && matchesFilter(t),
    ),
  );

  function setTasks(list: Task[]) {
    tasks.value = list;
    loading.value = false;
  }

  function updateTask(updated: Task) {
    const idx = tasks.value.findIndex(t => t.id === updated.id);
    if (idx >= 0) {
      tasks.value[idx] = updated;
    } else {
      tasks.value.push(updated);
    }
  }

  function removeTask(id: string) {
    tasks.value = tasks.value.filter(t => t.id !== id);
  }

  /** Fetch tasks for the active workspace. When `includeArchived` is true the
   *  endpoint also returns the most recent page of archived tasks; the page
   *  size is sourced from the server's `archived_tasks_per_page` env value
   *  (see SettingsTabExecution) and clamped server-side to [1, 200]. */
  async function fetchTasks(opts?: { includeArchived?: boolean; archivedPageSize?: number }) {
    try {
      let url = '/api/tasks';
      if (opts?.includeArchived) {
        const size = opts.archivedPageSize ?? 50;
        url += `?include_archived=true&archived_page_size=${size}`;
      }
      const resp = await api<Task[] | { tasks: Task[] }>('GET', url);
      const list = Array.isArray(resp) ? resp : (resp?.tasks ?? []);
      setTasks(list);
    } catch (e) {
      console.error('fetchTasks:', e);
      loading.value = false;
    }
  }

  async function fetchConfig() {
    try {
      config.value = await api<ServerConfig>('GET', '/api/config');
    } catch (e) {
      console.error('fetchConfig:', e);
    }
  }

  async function createTask(
    prompt: string,
    opts?: {
      flow?: string;
      timeout?: number;
      tags?: string[];
      model?: string;
      maxCostUsd?: number;
      maxInputTokens?: number;
    },
  ) {
    const body: Record<string, unknown> = { prompt, timeout: opts?.timeout ?? 900 };
    if (opts?.flow) body.flow = opts.flow;
    if (opts?.tags?.length) body.tags = opts.tags;
    if (opts?.model) body.model = opts.model;
    if (opts?.maxCostUsd && opts.maxCostUsd > 0) body.max_cost_usd = opts.maxCostUsd;
    if (opts?.maxInputTokens && opts.maxInputTokens > 0) body.max_input_tokens = opts.maxInputTokens;
    return api<Task>('POST', '/api/tasks', body);
  }

  /** Create up to 50 tasks in a single round-trip via POST /api/tasks/batch.
   *  All entries share the same flow / tags / timeout / model / budget; the
   *  server rejects per-task sandbox overrides, follow up with PATCH if needed.
   */
  async function batchCreateTasks(
    prompts: string[],
    opts?: {
      flow?: string;
      timeout?: number;
      tags?: string[];
      model?: string;
      maxCostUsd?: number;
      maxInputTokens?: number;
    },
  ) {
    const tasks = prompts.map((prompt) => {
      const t: Record<string, unknown> = { prompt, timeout: opts?.timeout ?? 900 };
      if (opts?.flow) t.flow = opts.flow;
      if (opts?.tags?.length) t.tags = opts.tags;
      if (opts?.model) t.model = opts.model;
      if (opts?.maxCostUsd && opts.maxCostUsd > 0) t.max_cost_usd = opts.maxCostUsd;
      if (opts?.maxInputTokens && opts.maxInputTokens > 0) t.max_input_tokens = opts.maxInputTokens;
      return t;
    });
    return api<{ tasks: Task[] }>('POST', '/api/tasks/batch', { tasks });
  }

  async function patchTask(id: string, patch: Record<string, unknown>) {
    return api<Task>('PATCH', `/api/tasks/${id}`, patch);
  }

  async function cancelTask(id: string) {
    return api<void>('POST', `/api/tasks/${id}/cancel`);
  }

  async function deleteTask(id: string) {
    return api<void>('DELETE', `/api/tasks/${id}`);
  }

  return {
    tasks, config, loading, filterQuery,
    backlog, inProgress, waiting, done,
    setTasks, updateTask, removeTask,
    fetchTasks, fetchConfig,
    createTask, batchCreateTasks, patchTask, cancelTask, deleteTask,
  };
});
