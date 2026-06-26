// When the user views a thread the server reports as running (busyThreadId ===
// activeThreadId) and we're not already streaming it, the session re-attaches to
// its live stream — otherwise returning to an in-progress session shows it empty
// (the in-flight turn isn't persisted yet). It must NOT attach when a different
// thread is the busy one.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, defineComponent, h, nextTick, type App } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { useChatSession } from './useChatSession';
import { useAgentStore } from '../stores/agentSession';

function streamFetchCount(): number {
  const f = globalThis.fetch as unknown as { mock: { calls: unknown[][] } };
  return f.mock.calls.filter((c) => String(c[0]).includes('/api/agent/messages/stream')).length;
}

const Harness = defineComponent({
  setup() {
    useChatSession();
    return () => h('div');
  },
});

let app: App | null = null;

describe('useChatSession live-stream re-attach', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setActivePinia(createPinia());
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes('/api/agent/sessions')) {
        return new Response(JSON.stringify({ threads: [], active_id: '' }), { status: 200 });
      }
      if (url.includes('/api/agent/messages/stream')) {
        return new Response(null, { status: 204 });
      }
      return new Response('[]', { status: 200 });
    }) as never;
  });

  afterEach(() => {
    app?.unmount();
    app = null;
    vi.useRealTimers();
  });

  async function mount() {
    app = createApp(Harness);
    app.mount(document.createElement('div'));
    await vi.runOnlyPendingTimersAsync();
    await nextTick();
  }

  it('attaches when the active thread is the busy one', async () => {
    await mount();
    const agentStore = useAgentStore();
    agentStore.activeThreadId = 'thread-x';
    agentStore.busyThreadId = 'thread-x';
    await nextTick();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(0);
    expect(streamFetchCount()).toBe(1);
  });

  it('does not attach when a different thread is busy', async () => {
    await mount();
    const agentStore = useAgentStore();
    agentStore.activeThreadId = 'thread-x';
    agentStore.busyThreadId = 'thread-y';
    await nextTick();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(0);
    expect(streamFetchCount()).toBe(0);
  });

  // The reported bug: chatting while an agent runs, switch to another tab, then
  // back — the live turn appears gone. On remount the store already reports the
  // active thread busy (values unchanged across the switch), so the change-driven
  // watch never fires; the shared `streaming` flag is also left stale-true with no
  // reader. Re-attach must still happen on mount.
  it('re-attaches on remount when the active thread is still busy', async () => {
    // Pre-seed the store as a torn-down reader would leave it: active thread is
    // busy, streaming flag stuck true, but no live reader in this fresh instance.
    const agentStore = useAgentStore();
    agentStore.activeThreadId = 'thread-x';
    agentStore.busyThreadId = 'thread-x';
    agentStore.streaming = true;
    agentStore.streamingThreadId = 'thread-x';

    // The server still reports the thread running, and loadThreads must keep the
    // pre-seeded ids unchanged so the watch stays silent (no spurious fire).
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes('/api/agent/sessions')) {
        return new Response(
          JSON.stringify({
            threads: [{ id: 'thread-x', name: 'Chat 1' }],
            active_id: 'thread-x',
            busy_thread_id: 'thread-x',
          }),
          { status: 200 },
        );
      }
      if (url.includes('/api/agent/messages/stream')) {
        return new Response(null, { status: 204 });
      }
      return new Response('[]', { status: 200 });
    }) as never;

    await mount();
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(0);
    // At least one attach. (The 204 test stub triggers the streaming retry, so
    // the exact count is incidental; production streams return data and don't
    // retry. Without the mount-time re-attach this is 0.)
    expect(streamFetchCount()).toBeGreaterThanOrEqual(1);
  });

  // The docked tab surface does not poll refreshBusy, so a turn it starts must
  // mark its own thread busy — otherwise switching tabs/threads away and back
  // cannot tell the turn is still running. sendMessage sets busyThreadId; the
  // re-attach above then has something to key on.
  it('marks the thread busy when a turn starts', async () => {
    let chat: ReturnType<typeof useChatSession> | null = null;
    const CaptureHarness = defineComponent({
      setup() {
        chat = useChatSession();
        return () => h('div');
      },
    });
    app = createApp(CaptureHarness);
    app.mount(document.createElement('div'));
    await vi.runOnlyPendingTimersAsync();
    await nextTick();

    const agentStore = useAgentStore();
    agentStore.threads = { 'thread-x': { id: 'thread-x', name: 'Chat 1', mode: '' } as never };
    agentStore.activeThreadId = 'thread-x';
    agentStore.busyThreadId = '';

    await chat!.sendMessage('hello');
    expect(agentStore.busyThreadId).toBe('thread-x');
  });
});
