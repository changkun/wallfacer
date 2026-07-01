// Per-task GitHub PR state (task-centric redesign): a task is a branch in a
// repo, so its PR is metadata on the task. This store caches the PR per task id
// and exposes create / status / comment against the task-scoped endpoints
// (/api/tasks/{id}/pr). The connection state lives in the separate github store.
import { defineStore } from 'pinia';
import { ref } from 'vue';

import { api } from '../api/client';

export interface TaskPullRequest {
  number: number;
  title: string;
  state: string;
  author: string;
  draft?: boolean;
  html_url?: string;
  body?: string;
}

export const useGithubPrStore = defineStore('githubPr', () => {
  // taskId -> PR (null = checked, none exists; undefined = not yet checked).
  const byTask = ref<Record<string, TaskPullRequest | null>>({});
  const loading = ref<Record<string, boolean>>({});
  const error = ref<string | null>(null);

  function prFor(taskId: string): TaskPullRequest | null | undefined {
    return byTask.value[taskId];
  }

  // fetchTaskPR loads the task's PR status. A task without a GitHub branch (400)
  // resolves to "no PR" quietly rather than surfacing an error.
  async function fetchTaskPR(taskId: string): Promise<void> {
    if (loading.value[taskId]) return;
    loading.value[taskId] = true;
    try {
      const resp = await api<{ pull_request: TaskPullRequest | null }>('GET', `/api/tasks/${taskId}/pr`);
      byTask.value[taskId] = resp.pull_request ?? null;
    } catch {
      byTask.value[taskId] = null;
    } finally {
      loading.value[taskId] = false;
    }
  }

  async function createTaskPR(
    taskId: string,
    opts?: { title?: string; body?: string; draft?: boolean },
  ): Promise<TaskPullRequest | null> {
    error.value = null;
    loading.value[taskId] = true;
    try {
      const pr = await api<TaskPullRequest>('POST', `/api/tasks/${taskId}/pr`, opts ?? {});
      byTask.value[taskId] = pr;
      return pr;
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
      return null;
    } finally {
      loading.value[taskId] = false;
    }
  }

  async function commentTaskPR(taskId: string, body: string): Promise<boolean> {
    error.value = null;
    if (!body.trim()) return false;
    try {
      await api('POST', `/api/tasks/${taskId}/pr/comment`, { body });
      return true;
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e);
      return false;
    }
  }

  return { byTask, loading, error, prFor, fetchTaskPR, createTaskPR, commentTaskPR };
});
