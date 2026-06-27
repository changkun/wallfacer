// Streams a task's agent output from GET /api/tasks/{id}/logs and exposes it
// both as raw NDJSON text and as a parsed, rendered trajectory (activity rows +
// the answer prose). The endpoint serves text/plain (NOT SSE) — live container
// output for running tasks, and saved turn outputs for completed tasks — so we
// read it with a chunked fetch reader (startStreamingFetch), not EventSource.
//
// Harness-aware rendering: Claude is parsed on the client by prettyNdjson (its
// rich, grandfathered parser); every other harness is rendered from the
// backend's normalized event stream (?format=normalized), which runs each
// harness's native format through harness.ParseEvent server-side. The raw view
// always shows the harness-native stream; toggling to it on a non-Claude
// harness refetches the un-normalized log.

import { ref, watch, onUnmounted } from 'vue';
import type { Ref } from 'vue';
import { startStreamingFetch, type StreamingFetchHandle } from './useStreamingFetch';
import { createTurnParser, type ActivityRow } from '../lib/prettyNdjson';
import { createNormalizedParser } from '../lib/normalizedActivity';

// The backend injects this sentinel once when a turn's output exceeds the 8MB
// cap (see store SaveTurnOutput). It appears once and never disappears.
const TRUNCATION_SENTINEL = '"subtype":"truncation_notice"';

export type TranscriptView = 'rendered' | 'raw';

export interface UseTaskActivityOptions {
  // The task's harness id (e.g. 'claude', 'codex'). Defaults to 'claude'.
  harness?: Ref<string | undefined>;
  // Which view the user is looking at. Defaults to 'rendered'. Only affects
  // non-Claude harnesses (Claude renders and shows raw from the same fetch).
  mode?: Ref<TranscriptView>;
}

type Strategy = 'claude' | 'normalized' | 'raw';

export function useTaskActivity(taskId: Ref<string | null>, opts: UseTaskActivityOptions = {}) {
  const raw = ref('');
  const activity = ref<ActivityRow[]>([]);
  const answer = ref('');
  const streaming = ref(false);
  // Sticky truncation flag: flips true the first time a chunk carries the
  // sentinel. Avoids rescanning the whole accumulated log on every chunk.
  const truncated = ref(false);
  let handle: StreamingFetchHandle | null = null;

  function stop() {
    handle?.abort();
    handle = null;
    streaming.value = false;
  }

  function plan(id: string): { url: string; strategy: Strategy } {
    const harness = (opts.harness?.value || 'claude').toLowerCase();
    const mode = opts.mode?.value ?? 'rendered';
    const base = `/api/tasks/${id}/logs`;
    if (harness === 'claude') return { url: base, strategy: 'claude' };
    if (mode === 'rendered') return { url: `${base}?format=normalized`, strategy: 'normalized' };
    return { url: base, strategy: 'raw' };
  }

  function start(id: string) {
    stop();
    raw.value = '';
    activity.value = [];
    answer.value = '';
    truncated.value = false;
    streaming.value = true;

    const { url, strategy } = plan(id);
    // Tail of the previous chunk so a sentinel split across a chunk boundary is
    // still detected without rescanning the whole buffer.
    let sentinelTail = '';
    // Incremental parser chosen by strategy: parse each line once instead of
    // re-parsing the accumulated buffer on every chunk (O(n^2) otherwise).
    const turn = strategy === 'claude' ? createTurnParser() : null;
    const norm = strategy === 'normalized' ? createNormalizedParser() : null;

    handle = startStreamingFetch({
      url,
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
        if (turn) {
          turn.push(chunk);
          activity.value = [...turn.rows()];
          answer.value = turn.answer();
        } else if (norm) {
          norm.push(chunk);
          activity.value = [...norm.rows()];
          answer.value = norm.answer();
        }
      },
      onDone: () => {
        if (turn) {
          turn.finalize();
          activity.value = [...turn.rows()];
          answer.value = turn.answer();
        } else if (norm) {
          norm.finalize();
          activity.value = [...norm.rows()];
          answer.value = norm.answer();
        }
        streaming.value = false;
      },
      onError: () => { streaming.value = false; },
    });
  }

  // Re-fetch when the task, the harness, or the view changes (the latter two
  // can flip the endpoint between raw and normalized).
  watch(
    [taskId, () => opts.harness?.value, () => opts.mode?.value],
    ([id]) => {
      if (id) start(id);
      else stop();
    },
    { immediate: true },
  );

  onUnmounted(stop);

  return { raw, activity, answer, streaming, truncated };
}
