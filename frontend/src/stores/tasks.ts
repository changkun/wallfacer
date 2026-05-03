import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import { api } from '../api/client';
import type { Task, ServerConfig } from '../api/types';

export const useTaskStore = defineStore('tasks', () => {
  const tasks = ref<Task[]>([]);
  const config = ref<ServerConfig | null>(null);
  const loading = ref(true);
  const filterQuery = ref('');

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
      (t.status === 'done' || t.status === 'cancelled') && !t.archived && matchesFilter(t),
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

  async function fetchTasks() {
    try {
      const list = await api<Task[]>('GET', '/api/tasks');
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

  async function createTask(prompt: string, opts?: { flow?: string; timeout?: number; tags?: string[] }) {
    const body: Record<string, unknown> = { prompt, timeout: opts?.timeout ?? 900 };
    if (opts?.flow) body.flow = opts.flow;
    if (opts?.tags?.length) body.tags = opts.tags;
    return api<Task>('POST', '/api/tasks', body);
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
    createTask, patchTask, cancelTask, deleteTask,
  };
});
