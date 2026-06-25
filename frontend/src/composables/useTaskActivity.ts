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
import { createActivityParser, type ActivityRow } from '../lib/prettyNdjson';

// The backend injects this sentinel once when a turn's output exceeds the 8MB
// cap (see store SaveTurnOutput). It appears once and never disappears.
const TRUNCATION_SENTINEL = '"subtype":"truncation_notice"';

export function useTaskActivity(taskId: Ref<string | null>) {
  const raw = ref('');
  const activity = ref<ActivityRow[]>([]);
  const streaming = ref(false);
  // Sticky truncation flag: flips true the first time a chunk carries the
  // sentinel. Avoids rescanning the whole accumulated log (up to ~8MB) on
  // every chunk (O(n^2)); the consumer reads this ref directly.
  const truncated = ref(false);
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
    truncated.value = false;
    streaming.value = true;
    // Tail of the previous chunk, so a sentinel split across a chunk boundary
    // is still detected without rescanning the whole buffer.
    let sentinelTail = '';
    // Incremental parser: parse each NDJSON line once instead of re-parsing the
    // whole accumulated buffer on every chunk (which is O(n^2) in frames).
    const parser = createActivityParser();
    handle = startStreamingFetch({
      url: `/api/tasks/${id}/logs`,
      onChunk: (chunk) => {
        raw.value += chunk;
        if (!truncated.value) {
          if ((sentinelTail + chunk).includes(TRUNCATION_SENTINEL)) {
            truncated.value = true;
            sentinelTail = '';
          } else {
            sentinelTail = chunk.slice(-TRUNCATION_SENTINEL.length);
          }
        }
        activity.value = [...parser.push(chunk)];
      },
      onDone: () => {
        activity.value = [...parser.finalize()];
        streaming.value = false;
      },
      onError: () => { streaming.value = false; },
    });
  }

  watch(taskId, (id) => {
    if (id) start(id);
    else stop();
  }, { immediate: true });

  onUnmounted(stop);

  return { raw, activity, streaming, truncated };
}
