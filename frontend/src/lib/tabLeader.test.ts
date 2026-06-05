import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

// Regression test for the multi-tab SSE connection exhaustion bug.
//
// HTTP/1.1 caps ~6 connections per origin. Before the tab-leader relay every
// tab opened its own EventSource for each stream, so 3+ tabs froze the data
// pages. The relay elects ONE leader tab that holds the real EventSource and
// relays events to followers. These tests prove:
//   1. N tabs construct exactly ONE real EventSource (not N).
//   2. A relayed event reaches every follower's listener.
//   3. On leader close, a follower is promoted and opens a second EventSource.
//
// Each "tab" is a fresh module graph (vi.resetModules + dynamic import) so the
// tabLeader singleton state is per-tab, while a shared in-memory bus stands in
// for the real cross-tab BroadcastChannel.

// --- Shared in-memory BroadcastChannel bus ---------------------------------

interface BusChannel {
  name: string;
  instances: Set<MockBroadcastChannel>;
}

const buses = new Map<string, BusChannel>();

class MockBroadcastChannel {
  onmessage: ((e: MessageEvent) => void) | null = null;
  private bus: BusChannel;
  private closed = false;

  constructor(public name: string) {
    let bus = buses.get(name);
    if (!bus) {
      bus = { name, instances: new Set() };
      buses.set(name, bus);
    }
    this.bus = bus;
    this.bus.instances.add(this);
  }

  postMessage(data: unknown): void {
    if (this.closed) return;
    // Real BroadcastChannel does NOT deliver to the sender — only to other
    // instances on the same channel name. Deliver synchronously so the election
    // resolves deterministically under fake timers.
    for (const inst of this.bus.instances) {
      if (inst === this || inst.closed) continue;
      inst.onmessage?.({ data } as MessageEvent);
    }
  }

  close(): void {
    this.closed = true;
    this.bus.instances.delete(this);
  }
}

// --- EventSource spy --------------------------------------------------------

let esConstructed = 0;
const esInstances: MockEventSource[] = [];

class MockEventSource {
  static readonly OPEN = 1;
  onopen: ((e: Event) => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onerror: ((e: Event) => void) | null = null;
  private listeners = new Map<string, Set<EventListener>>();
  closed = false;

  constructor(public url: string) {
    esConstructed += 1;
    esInstances.push(this);
  }

  addEventListener(type: string, cb: EventListener): void {
    let set = this.listeners.get(type);
    if (!set) { set = new Set(); this.listeners.set(type, set); }
    set.add(cb);
  }

  // Test helper: simulate a named SSE event arriving on the real connection.
  emit(type: string, data: string, lastEventId = ''): void {
    const ev = { data, lastEventId } as MessageEvent;
    for (const cb of this.listeners.get(type) ?? []) cb(ev as Event);
  }

  close(): void { this.closed = true; }
}

// A single tab: a fresh tabLeader + useSse, sharing the mock bus.
interface Tab {
  connState: { value: string };
  received: Array<{ data: unknown; lastEventId: string }>;
  // Number of times this tab refetched its source-of-truth (onStaleRestart) —
  // followers fire it once on subscribe to seed the snapshot they missed.
  seedCount: number;
  closeTab: () => void;
}

async function spawnTab(): Promise<Tab> {
  vi.resetModules();
  // Real tabs each own a separate window, so one tab's unload fires only its
  // own listeners. happy-dom shares a single global window across the reset
  // module graphs, so capture THIS tab's beforeunload handler at import time
  // and let closeTab invoke just that one.
  let beforeUnload: (() => void) | null = null;
  const origAdd = window.addEventListener.bind(window);
  const addSpy = vi
    .spyOn(window, 'addEventListener')
    .mockImplementation((type: string, cb: EventListenerOrEventListenerObject, opts?: unknown) => {
      if (type === 'beforeunload') {
        beforeUnload = () => (cb as EventListener)(new Event('beforeunload'));
        return;
      }
      return origAdd(type, cb as EventListener, opts as never);
    });

  const { useSse } = await import('../composables/useSse');
  const received: Tab['received'] = [];
  const seed = { count: 0 };
  const { connState } = useSse({
    url: '/api/tasks/stream',
    listeners: {
      snapshot: (data, lastEventId) => { received.push({ data, lastEventId }); },
    },
    staleThresholdMs: 0, // disable watchdog noise in tests
    onStaleRestart: () => { seed.count += 1; },
  });
  addSpy.mockRestore();

  // The election timer (250 ms) decides leadership; let it settle.
  await vi.advanceTimersByTimeAsync(300);
  return {
    connState: connState as unknown as { value: string },
    received,
    get seedCount() { return seed.count; },
    closeTab: () => beforeUnload?.(),
  };
}

describe('tabLeader cross-tab SSE relay', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    buses.clear();
    esConstructed = 0;
    esInstances.length = 0;
    vi.stubGlobal('BroadcastChannel', MockBroadcastChannel);
    vi.stubGlobal('EventSource', MockEventSource);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.resetModules();
  });

  it('opens exactly ONE EventSource across N tabs and relays to followers', async () => {
    const N = 4;
    const tabs: Tab[] = [];
    // Stagger tab creation: tab1 wins the election and opens the real stream,
    // then tabs 2..N see `i-am-leader` and stay followers. (Creating them all
    // at once would expire every election timer together and elect N leaders.)
    for (let i = 0; i < N; i++) tabs.push(await spawnTab());

    expect(esConstructed).toBe(1);

    // The leader's real EventSource receives a snapshot; followers must get it
    // via the relay.
    const leaderEs = esInstances[0];
    leaderEs.onopen?.(new Event('open'));
    leaderEs.emit('snapshot', JSON.stringify([{ id: 'a' }]), 'evt-1');

    for (const tab of tabs) {
      expect(tab.received).toHaveLength(1);
      expect(tab.received[0]).toEqual({
        data: [{ id: 'a' }],
        lastEventId: 'evt-1',
      });
    }
  });

  it('promotes a follower to leader when the leader tab closes (failover)', async () => {
    const leaderTab = await spawnTab();
    const followerTab = await spawnTab();
    void leaderTab;
    expect(esConstructed).toBe(1);

    // Leader leaves. Followers re-run the election (random <150 ms delay) and
    // one opens a SECOND real EventSource.
    leaderTab.closeTab();
    await vi.advanceTimersByTimeAsync(500);

    expect(esConstructed).toBe(2);

    // The promoted tab now drives a real stream; a fresh event reaches it.
    const newEs = esInstances[1];
    newEs.onopen?.(new Event('open'));
    newEs.emit('snapshot', JSON.stringify([{ id: 'b' }]), 'evt-2');
    expect(followerTab.received.at(-1)).toEqual({
      data: [{ id: 'b' }],
      lastEventId: 'evt-2',
    });
  });

  it('elects exactly ONE new leader on failover with multiple followers', async () => {
    // With 2+ followers, both re-run the election after `leader-leaving`. Each
    // schedules a claim 250 ms after its own random back-off; if claiming by
    // timeout doesn't announce leadership, the second claimant never hears the
    // first and self-elects too — re-creating the original pool-exhaustion bug
    // (two leaders → two real streams). This guards that regression.
    const leaderTab = await spawnTab();
    await spawnTab(); // follower 1
    await spawnTab(); // follower 2
    expect(esConstructed).toBe(1);

    leaderTab.closeTab();
    // Cover the full back-off window (<150 ms) plus the 250 ms claim timer with
    // headroom so every pending election resolves.
    await vi.advanceTimersByTimeAsync(600);

    expect(esConstructed).toBe(2);
  });

  it('seeds a late-opening follower that missed the leader snapshot', async () => {
    // Leader opens alone and the server's one-shot `snapshot` fires before any
    // follower exists. A follower opening later never sees that snapshot over
    // the relay, so it must refetch its source-of-truth (onStaleRestart) once
    // on subscribe — otherwise it renders an empty board.
    const leaderTab = await spawnTab();
    const leaderEs = esInstances[0];
    leaderEs.onopen?.(new Event('open'));
    leaderEs.emit('snapshot', JSON.stringify([{ id: 'a' }]), 'evt-1');
    expect(leaderTab.received).toHaveLength(1);

    // Now a second tab opens. It becomes a follower (no new EventSource) and
    // must seed exactly once to recover the snapshot it missed.
    const followerTab = await spawnTab();
    expect(esConstructed).toBe(1);
    expect(followerTab.seedCount).toBe(1);
    // It got nothing over the relay (the snapshot already passed).
    expect(followerTab.received).toHaveLength(0);

    // A subsequent live delta still reaches it.
    leaderEs.emit('snapshot', JSON.stringify([{ id: 'a' }, { id: 'b' }]), 'evt-2');
    expect(followerTab.received.at(-1)).toEqual({
      data: [{ id: 'a' }, { id: 'b' }],
      lastEventId: 'evt-2',
    });
  });
});
