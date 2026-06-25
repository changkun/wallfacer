// Regression: the streaming retry setTimeout (scheduled on an empty stream via
// onDone(hadData=false)) was never tracked or cancelled. If the component
// unmounted, the user interrupted, or switched threads during the 500ms retry
// window, the timer still fired connect(), re-opening a stream for a thread the
// user had already left. The fix tracks the timer and clears it in
// finishStreaming, the switchToThread detach branch, and onUnmounted.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, defineComponent, h, nextTick, type App } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { useChatSession } from './useChatSession';

function streamFetchCount(): number {
  const f = globalThis.fetch as unknown as { mock: { calls: unknown[][] } };
  return f.mock.calls.filter((c) => String(c[0]).includes('/api/agent/messages/stream')).length;
}

let session: ReturnType<typeof useChatSession> | null = null;

const Harness = defineComponent({
  setup() {
    session = useChatSession();
    return () => h('div');
  },
});

let app: App | null = null;

describe('useChatSession streaming retry timer', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setActivePinia(createPinia());
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.includes('/api/agent/sessions')) {
        return new Response(JSON.stringify({ threads: [], active_id: '' }), { status: 200 });
      }
      // POST to start a message: accept so startStreaming() runs.
      if (url.endsWith('/api/agent/messages') && init?.method === 'POST') {
        return new Response('{}', { status: 200 });
      }
      // The stream endpoint replies 204 (no content), which drives
      // onDone(hadData=false) and schedules the single retry.
      if (url.includes('/api/agent/messages/stream')) {
        return new Response(null, { status: 204 });
      }
      return new Response('[]', { status: 200 });
    }) as never;
  });

  afterEach(() => {
    app?.unmount();
    app = null;
    session = null;
    vi.useRealTimers();
  });

  it('does not reconnect after unmount during the retry window', async () => {
    const host = document.createElement('div');
    app = createApp(Harness);
    app.mount(host);
    // Flush onMounted's loadThreads/loadHistory.
    await vi.runOnlyPendingTimersAsync();
    await nextTick();

    // Start a stream against an explicit thread id (bypasses active-thread setup).
    await session!.sendMessage('hello', { threadID: 'thread-x' });
    // Flush only microtasks (NOT the 500ms retry timer) so the POST resolves
    // and the stream fetch runs to its 204 -> onDone(false), which schedules
    // the retry. advanceTimersByTimeAsync(0) drains pending promise jobs
    // without firing the still-pending 500ms timer.
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(0);
    await nextTick();

    const afterFirst = streamFetchCount();
    expect(afterFirst).toBe(1); // one stream attempt, retry still pending

    // Unmount during the 500ms retry window, then fire all timers.
    app.unmount();
    app = null;
    await vi.advanceTimersByTimeAsync(1000);
    await nextTick();

    // Without the fix, the pending retry timer fired connect() and re-opened
    // the stream after teardown. With the fix it was cleared on unmount.
    expect(streamFetchCount()).toBe(afterFirst);
  });
});
