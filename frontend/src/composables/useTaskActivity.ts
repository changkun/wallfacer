// Streams a task's agent output from GET /api/tasks/{id}/logs and exposes it
// both as raw NDJSON text and as parsed pretty-activity rows. The endpoint
// serves text/plain (NOT SSE) — live container output for running tasks, and
// saved turn outputs for completed tasks — so we read it with a chunked fetch
// reader (startStreamingFetch), not EventSource. This is what makes a finished
// task's output viewable (the old useLogStream/EventSource path only worked,
// at best, for live tasks and never replayed completed output).

import { ref, watch, onUnmounted } from 'vue';
import type { Ref } from 'vue';
import { startStreamingFetch, type StreamingFetchHandle } from './useStreamingFetch';
import { parseActivity, type ActivityRow } from '../lib/prettyNdjson';

export function useTaskActivity(taskId: Ref<string | null>) {
  const raw = ref('');
  const activity = ref<ActivityRow[]>([]);
  const streaming = ref(false);
  let handle: StreamingFetchHandle | null = null;

  function stop() {
    handle?.abort();
    handle = null;
    streaming.value = false;
  }

  function start(id: string) {
    stop();
    raw.value = '';
    activity.value = [];
    streaming.value = true;
    handle = startStreamingFetch({
      url: `/api/tasks/${id}/logs`,
      onChunk: (chunk) => {
        raw.value += chunk;
        activity.value = parseActivity(raw.value);
      },
      onDone: () => { streaming.value = false; },
      onError: () => { streaming.value = false; },
    });
  }

  watch(taskId, (id) => {
    if (id) start(id);
    else stop();
  }, { immediate: true });

  onUnmounted(stop);

  return { raw, activity, streaming };
}
