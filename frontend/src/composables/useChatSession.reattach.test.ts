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
});
