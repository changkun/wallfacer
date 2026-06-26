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
import { useAgentStore } from '../stores/agentSession';

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
    ([url, init]) => url.endsWith('/api/agent/sessions') && (init?.method ?? 'GET').toUpperCase() === 'POST',
  ).length;
}

function messagePostCount(): number {
  return fetchCalls().filter(
    ([url, init]) =>
      url.startsWith('/api/agent/messages') &&
      !url.includes('/stream') &&
      (init?.method ?? 'GET').toUpperCase() === 'POST',
  ).length;
}

let app: App | null = null;
let createSeq = 0;
let createdThreads: Array<{ id: string; name: string; archived: boolean }> = [];
// When set, the next history GET blocks on this promise so a test can force it
// to resolve after a later send (reproducing the stale-load wipe).
let messagesGate: Promise<void> | null = null;
let releaseGate: (() => void) | null = null;

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
    createdThreads = [];
    messagesGate = null;
    releaseGate = null;
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      const method = (init?.method ?? 'GET').toUpperCase();
      if (url.endsWith('/api/agent/sessions') && method === 'POST') {
        createSeq++;
        const t = { id: 'new-' + createSeq, name: 'Chat ' + createSeq, archived: false };
        createdThreads.push(t);
        return new Response(JSON.stringify(t), { status: 201 });
      }
      if (url.includes('/api/agent/sessions') && method === 'PATCH') {
        return new Response(null, { status: 200 });
      }
      if (url.includes('/api/agent/sessions')) {
        // Mirror the server: list reflects created threads. active_id stays ''
        // (Create does not activate server-side), so loadThreads during promotion
        // sets activeThreadId to the new thread itself — the harshest race path.
        return new Response(JSON.stringify({ threads: createdThreads, active_id: '' }), { status: 200 });
      }
      if (url.includes('/api/agent/messages/stream')) {
        return new Response(null, { status: 204 });
      }
      if (url.startsWith('/api/agent/messages') && method === 'POST') {
        return new Response(JSON.stringify({ status: 'accepted' }), { status: 202 });
      }
      if (url.startsWith('/api/agent/messages')) {
        // loadHistory: thread "a" has prior content; everything else is empty.
        if (messagesGate) await messagesGate;
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
    const agentStore = useAgentStore();
    // Land on an existing thread with content so we can prove it blanks.
    agentStore.threads = {
      a: { id: 'a', name: 'Alpha', archived: false, mode: '', task_id: '', unread: false, scrollTop: 0, queue: [], enqueuedAt: 0, lastViewedAt: 0, created: 0, updated: 0 },
    };
    agentStore.threadOrder = ['a'];
    await session.switchToThread('a');
    await flush();
    expect(session.renderedMessages.value.length).toBe(1);

    await session.createThread();
    await flush();

    expect(session.draft.value).toBe(true);
    expect(agentStore.activeThreadId).toBe('');
    expect(session.renderedMessages.value.length).toBe(0);
    expect(threadCreateCount()).toBe(0);
  });

  it('creates the thread on first send and keeps the user bubble', async () => {
    await mount();
    const agentStore = useAgentStore();

    await session.createThread();
    await flush();
    expect(threadCreateCount()).toBe(0);

    await session.sendMessage('hello there');
    await flush();

    expect(threadCreateCount()).toBe(1);
    expect(messagePostCount()).toBe(1);
    expect(agentStore.activeThreadId).toBe('new-1');
    expect(session.draft.value).toBe(false);
    expect(session.renderedMessages.value.some((b) => b.role === 'user' && b.rawText === 'hello there')).toBe(true);
  });

  it('shows the first message as a provisional title instead of "Chat N"', async () => {
    await mount();
    const agentStore = useAgentStore();

    await session.createThread();
    await flush();

    // The created server thread is named "Chat 1"; the user must instead see
    // their prompt (truncated) until the backend auto-titler lands.
    await session.sendMessage('help me wire up the billing webhook end to end please');
    await flush();

    const t = agentStore.threads['new-1'];
    expect(t.name).toBe('help me wire up the billing webhook end to end…');
    expect(t.titlePending).toBe(true);
  });

  it('detaches a streaming thread on draft entry so the first message sends', async () => {
    await mount();
    const agentStore = useAgentStore();
    // Thread "a" is mid-stream when the user clicks "New chat".
    agentStore.streaming = true;
    agentStore.streamingThreadId = 'a';

    await session.createThread();
    await flush();
    expect(agentStore.streaming).toBe(false);

    await session.sendMessage('first message');
    await flush();

    // Promoted and sent — not enqueued into the detached stream.
    expect(threadCreateCount()).toBe(1);
    expect(messagePostCount()).toBe(1);
    expect(agentStore.threads['new-1']?.queue.length ?? 0).toBe(0);
  });

  it('a stale history load that resolves after a send does not wipe the message', async () => {
    await mount();
    const agentStore = useAgentStore();
    agentStore.threads = {
      a: { id: 'a', name: 'Alpha', archived: false, mode: '', task_id: '', unread: false, scrollTop: 0, queue: [], enqueuedAt: 0, lastViewedAt: 0, created: 0, updated: 0 },
    };
    agentStore.threadOrder = ['a'];

    // Block the history GET, then activate "a" so its load hangs in flight.
    messagesGate = new Promise<void>((r) => { releaseGate = r; });
    agentStore.activeThreadId = 'a';
    await nextTick();

    // Send while that load is still pending: bumps the history token.
    await session.sendMessage('hello');
    await nextTick();
    expect(session.renderedMessages.value.some((b) => b.role === 'user' && b.rawText === 'hello')).toBe(true);

    // The stale load now resolves with the server's prior list. The token guard
    // must drop it instead of replacing the just-sent message.
    releaseGate!();
    messagesGate = null;
    await flush();
    expect(session.renderedMessages.value.some((b) => b.role === 'user' && b.rawText === 'hello')).toBe(true);
  });

  it('a message queued mid-turn survives the post-turn history reload when it drains', async () => {
    await mount();
    const agentStore = useAgentStore();
    agentStore.threads = {
      a: { id: 'a', name: 'Alpha', archived: false, mode: '', task_id: '', unread: false, scrollTop: 0, queue: [], enqueuedAt: 0, lastViewedAt: 0, created: 0, updated: 0 },
    };
    agentStore.threadOrder = ['a'];
    await session.switchToThread('a');
    await flush();

    // Start a turn so the thread is streaming. The stream endpoint returns 204,
    // so the turn stays open via a scheduled retry rather than finishing.
    await session.sendMessage('first');
    await flush();
    expect(session.streaming.value).toBe(true);

    // Queue a follow-up while the turn is in flight.
    await session.sendMessage('queued');
    await flush();
    expect(agentStore.threads['a'].queue.length).toBe(1);

    // Gate the history reload that the turn's completion will trigger, so we can
    // resolve it after the queued message drains (reproducing the wipe).
    messagesGate = new Promise<void>((r) => { releaseGate = r; });

    // Drive the stream to completion: the retry fires a second 204, which ends
    // the turn. finishStreaming drains the queued message (optimistic bubble)
    // and then reloads history.
    await vi.advanceTimersByTimeAsync(500);
    expect(session.renderedMessages.value.some((b) => b.role === 'user' && b.rawText === 'queued')).toBe(true);

    // The gated reload resolves with the server's pre-queue history. It must not
    // clobber the just-drained message.
    releaseGate!();
    messagesGate = null;
    await flush();
    expect(session.renderedMessages.value.some((b) => b.role === 'user' && b.rawText === 'queued')).toBe(true);
  });
});
