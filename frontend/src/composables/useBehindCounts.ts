// Lazy fetcher for per-task behind-upstream counts. Reads
// GET /api/tasks/{id}/diff and exposes the sum across workspaces. The
// result is cached by (taskId, updatedAt) so consecutive renders don't
// re-hit the network until the task's state actually changes.
//
// Cards only call this when the task is in a status where falling
// behind matters (waiting / failed) — the legacy ui/js/render.js
// surface mirrored this restriction.

import { ref, watch, type Ref } from 'vue';
import { api } from '../api/client';

interface CacheEntry { total: number; updatedAt: string }
const cache = new Map<string, CacheEntry>();

export function useBehindCounts(taskId: Ref<string>, updatedAt: Ref<string>) {
  const total = ref(0);

  async function load() {
    const id = taskId.value;
    if (!id) { total.value = 0; return; }
    const cached = cache.get(id);
    if (cached && cached.updatedAt === updatedAt.value) {
      total.value = cached.total;
      return;
    }
    try {
      const data = await api<{ behind_counts?: Record<string, number> }>(
        'GET',
        `/api/tasks/${id}/diff`,
      );
      const counts = data?.behind_counts ?? {};
      let sum = 0;
      for (const n of Object.values(counts)) sum += n;
      cache.set(id, { total: sum, updatedAt: updatedAt.value });
      total.value = sum;
    } catch {
      // Diff endpoint can 404 mid-rebase / mid-cleanup — treat as 0.
      total.value = 0;
    }
  }

  watch([taskId, updatedAt], load, { immediate: true });

  return { total };
}
