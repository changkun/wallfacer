// Deferred chat creation: "New chat" opens a blank draft and creates NO server
// thread; the thread is created (and later auto-titled by the backend) only on
// the first message. Covers the two interactions that value-only store tests
// miss: the optimistic user bubble must survive the promote-and-send sequence,
// and entering a draft while another thread is streaming must detach so the
// first message sends instead of being enqueued into the in-flight turn.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, defineComponent, h, nextTick, type App } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { useChatSession, type ChatSession } from './useChatSession';
import { usePlanningStore } from '../stores/planning';

let session: ChatSession;

const Harness = defineComponent({
  setup() {
    session = useChatSession();
    return () => h('div');
  },
});

function fetchCalls(): Array<[string, RequestInit | undefined]> {
  const f = globalThis.fetch as unknown as { mock: { calls: unknown[][] } };
  return f.mock.calls.map((c) => [String(c[0]), c[1] as RequestInit | undefined]);
}

function threadCreateCount(): number {
  return fetchCalls().filter(
    ([url, init]) => url.endsWith('/api/planning/threads') && (init?.method ?? 'GET').toUpperCase() === 'POST',
  ).length;
}

function messagePostCount(): number {
  return fetchCalls().filter(
    ([url, init]) =>
      url.startsWith('/api/planning/messages') &&
      !url.includes('/stream') &&
      (init?.method ?? 'GET').toUpperCase() === 'POST',
  ).length;
}

let app: App | null = null;
let createSeq = 0;

async function flush() {
  await nextTick();
  await vi.advanceTimersByTimeAsync(0);
  await nextTick();
}

describe('useChatSession deferred chat creation', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setActivePinia(createPinia());
    createSeq = 0;
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = (init?.method ?? 'GET').toUpperCase();
      if (url.endsWith('/api/planning/threads') && method === 'POST') {
        createSeq++;
        return new Response(JSON.stringify({ id: 'new-' + createSeq, name: 'Chat ' + createSeq, archived: false }), { status: 201 });
      }
      if (url.includes('/api/planning/threads') && method === 'PATCH') {
        return new Response(null, { status: 200 });
      }
      if (url.includes('/api/planning/threads')) {
        return new Response(JSON.stringify({ threads: [], active_id: '' }), { status: 200 });
      }
      if (url.includes('/api/planning/messages/stream')) {
        return new Response(null, { status: 204 });
      }
      if (url.startsWith('/api/planning/messages') && method === 'POST') {
        return new Response(JSON.stringify({ status: 'accepted' }), { status: 202 });
      }
      if (url.startsWith('/api/planning/messages')) {
        // loadHistory: thread "a" has prior content; everything else is empty.
        if (url.includes('thread=a')) {
          return new Response(JSON.stringify([{ role: 'user', content: 'old', timestamp: '2026-06-25T00:00:00Z' }]), { status: 200 });
        }
        return new Response('[]', { status: 200 });
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
    await flush();
  }

  it('opens a blank draft without creating a server thread', async () => {
    await mount();
    const planning = usePlanningStore();
    // Land on an existing thread with content so we can prove it blanks.
    planning.threads = {
      a: { id: 'a', name: 'Alpha', archived: false, mode: '', task_id: '', unread: false, scrollTop: 0, queue: [], enqueuedAt: 0, lastViewedAt: 0, created: 0, updated: 0 },
    };
    planning.threadOrder = ['a'];
    await session.switchToThread('a');
    await flush();
    expect(session.renderedMessages.value.length).toBe(1);

    await session.createThread();
    await flush();

    expect(session.draft.value).toBe(true);
    expect(planning.activeThreadId).toBe('');
    expect(session.renderedMessages.value.length).toBe(0);
    expect(threadCreateCount()).toBe(0);
  });

  it('creates the thread on first send and keeps the user bubble', async () => {
    await mount();
    const planning = usePlanningStore();

    await session.createThread();
    await flush();
    expect(threadCreateCount()).toBe(0);

    await session.sendMessage('hello there');
    await flush();

    expect(threadCreateCount()).toBe(1);
    expect(messagePostCount()).toBe(1);
    expect(planning.activeThreadId).toBe('new-1');
    expect(session.draft.value).toBe(false);
    expect(session.renderedMessages.value.some((b) => b.role === 'user' && b.rawText === 'hello there')).toBe(true);
  });

  it('detaches a streaming thread on draft entry so the first message sends', async () => {
    await mount();
    const planning = usePlanningStore();
    // Thread "a" is mid-stream when the user clicks "New chat".
    planning.streaming = true;
    planning.streamingThreadId = 'a';

    await session.createThread();
    await flush();
    expect(planning.streaming).toBe(false);

    await session.sendMessage('first message');
    await flush();

    // Promoted and sent — not enqueued into the detached stream.
    expect(threadCreateCount()).toBe(1);
    expect(messagePostCount()).toBe(1);
    expect(planning.threads['new-1']?.queue.length ?? 0).toBe(0);
  });
});
