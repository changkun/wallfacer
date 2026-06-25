// useTaskActivity streams a task's NDJSON log and parses it into activity rows.
// The parse must be incremental: re-parsing the whole accumulated buffer on
// every chunk is O(n^2) in frames for a long stream. These tests pin both the
// output equivalence and the linear parse-call count.

import { describe, it, expect, vi, afterEach } from 'vitest';
import { parseActivity } from '../lib/prettyNdjson';

// Capture the options passed to startStreamingFetch so the test can drive the
// onChunk/onDone callbacks directly without a real network stream.
let captured: { onChunk: (c: string) => void; onDone: () => void; onError: () => void } | null = null;
vi.mock('./useStreamingFetch', () => ({
  startStreamingFetch: (opts: { onChunk: (c: string) => void; onDone: () => void; onError: () => void }) => {
    captured = opts;
    return { abort: () => {} };
  },
}));

import { ref, nextTick } from 'vue';
import { useTaskActivity } from './useTaskActivity';
import { createActivityParser } from '../lib/prettyNdjson';

function frameLine(i: number): string {
  return JSON.stringify({
    type: 'assistant',
    message: { content: [{ type: 'tool_use', name: `tool-${i}`, input: { n: i } }] },
  }) + '\n';
}

afterEach(() => {
  captured = null;
  vi.restoreAllMocks();
});

describe('createActivityParser', () => {
  it('matches parseActivity over the full buffer regardless of chunking', () => {
    const full = Array.from({ length: 7 }, (_, i) => frameLine(i)).join('');
    const expected = parseActivity(full);

    // Feed the same bytes in awkward chunk boundaries (mid-line splits).
    const parser = createActivityParser();
    for (let i = 0; i < full.length; i += 5) parser.push(full.slice(i, i + 5));
    const got = parser.finalize();

    expect(got).toEqual(expected);
  });
});

describe('useTaskActivity', () => {
  it('parses each frame once (linear), not the whole buffer per chunk', () => {
    const taskId = ref<string | null>('t1');
    const { activity } = useTaskActivity(taskId);
    expect(captured).not.toBeNull();

    const N = 40;
    const parseSpy = vi.spyOn(JSON, 'parse');
    for (let i = 0; i < N; i++) captured!.onChunk(frameLine(i));
    captured!.onDone();

    // Output is correct.
    const full = Array.from({ length: N }, (_, i) => frameLine(i)).join('');
    expect(activity.value).toEqual(parseActivity(full));

    // Linear (a small constant per chunk), not quadratic. The old code
    // re-parsed every prior frame on each chunk (~N^2/2 ≈ 820 calls for N=40);
    // incremental parsing is O(N). N*4 cleanly separates the two.
    expect(parseSpy.mock.calls.length).toBeLessThanOrEqual(N * 4);
  });
});

const SENTINEL_LINE = JSON.stringify({ type: 'system', subtype: 'truncation_notice' }) + '\n';

describe('useTaskActivity truncation flag', () => {
  it('flips truncated true when a chunk carries the sentinel and stays sticky', () => {
    const taskId = ref<string | null>('t1');
    const { truncated } = useTaskActivity(taskId);
    expect(truncated.value).toBe(false);

    captured!.onChunk(frameLine(0));
    expect(truncated.value).toBe(false);

    captured!.onChunk(SENTINEL_LINE);
    expect(truncated.value).toBe(true);

    // A later chunk without the sentinel must not unset it.
    captured!.onChunk(frameLine(1));
    expect(truncated.value).toBe(true);
  });

  it('detects a sentinel split across a chunk boundary', () => {
    const taskId = ref<string | null>('t1');
    const { truncated } = useTaskActivity(taskId);
    const mid = Math.floor(SENTINEL_LINE.length / 2);
    captured!.onChunk(SENTINEL_LINE.slice(0, mid));
    captured!.onChunk(SENTINEL_LINE.slice(mid));
    expect(truncated.value).toBe(true);
  });

  it('clears the flag on task switch (start)', async () => {
    const taskId = ref<string | null>('t1');
    const { truncated } = useTaskActivity(taskId);
    captured!.onChunk(SENTINEL_LINE);
    expect(truncated.value).toBe(true);

    // Switching tasks re-invokes start(), which resets the flag (watcher is
    // async, so flush before asserting).
    taskId.value = 't2';
    await nextTick();
    expect(truncated.value).toBe(false);
  });
});
