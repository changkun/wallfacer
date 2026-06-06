import { ref, onUnmounted } from 'vue';
import { isLeader, onLeadershipChange, relay, subscribeAsFollower } from '../lib/tabLeader';
import { withAuthToken } from '../api/client';

/** Tri-state SSE health, mirroring the legacy status-bar conn dot. */
export type ConnState = 'ok' | 'reconnecting' | 'closed';

export interface UseSseOptions {
  url: string;
  withCredentials?: boolean;
  onMessage?: (event: MessageEvent) => void;
  listeners?: Record<string, (data: unknown, lastEventId: string) => void>;
  initialDelay?: number;
  maxDelay?: number;
  /** Watchdog: if no event (incl. heartbeat) arrives for this many ms,
   *  the connection is presumed dead and forcibly restarted. The server
   *  emits heartbeats every 15 s, so the default 35 s gives ~2× headroom.
   *  Set to 0 to disable. */
  staleThresholdMs?: number;
  /** How often to poll the staleness watchdog. Default 10 s. */
  stalenessCheckIntervalMs?: number;
  /** Called when the watchdog tripped and the stream is being restarted —
   *  callers typically refetch their source-of-truth to recover from a
   *  missed delta. */
  onStaleRestart?: () => void;
}

export function useSse(opts: UseSseOptions) {
  const connected = ref(false);
  // connState is the richer signal: 'reconnecting' while a retry is pending,
  // 'closed' when stopped or before the first connect, 'ok' once open.
  const connState = ref<ConnState>('closed');
  let es: EventSource | null = null;
  let retryDelay = opts.initialDelay ?? 1000;
  const maxDelay = opts.maxDelay ?? 30000;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let stopped = false;

  const staleMs = opts.staleThresholdMs ?? 35000;
  const stalenessIntervalMs = opts.stalenessCheckIntervalMs ?? 10000;
  let watchdog: ReturnType<typeof setInterval> | null = null;
  let lastEventAt = 0;

  // Per-stream relay namespace so the tasks and git streams don't cross-talk.
  const relayChannel = opts.url;
  // Track follower unsubscribes so leadership changes can tear them down.
  let followerUnsubs: Array<() => void> = [];

  const addAuthParam = withAuthToken;

  function markEvent() {
    lastEventAt = Date.now();
  }

  // Deliver a (data, lastEventId) pair to the configured listener AND relay it
  // to followers. The leader runs this for both raw and named events; followers
  // run the listener-only half via the relay subscription.
  function dispatch(name: string, data: unknown, lastEventId: string | null) {
    const handler = opts.listeners?.[name];
    if (handler) handler(data, lastEventId ?? '');
  }

  function restartStale() {
    es?.close();
    es = null;
    connected.value = false;
    opts.onStaleRestart?.();
    connectLeader();
  }

  // Leader path: open the real EventSource, drive listeners, relay each event.
  function connectLeader() {
    if (stopped) return;
    connState.value = 'reconnecting';
    es = new EventSource(addAuthParam(opts.url), {
      withCredentials: opts.withCredentials ?? true,
    });

    es.onopen = () => {
      connected.value = true;
      connState.value = 'ok';
      retryDelay = opts.initialDelay ?? 1000;
      markEvent();
    };

    es.onmessage = (ev) => {
      retryDelay = opts.initialDelay ?? 1000;
      markEvent();
      opts.onMessage?.(ev);
      // Relay default (unnamed) message events under a sentinel key so
      // followers can mirror onMessage consumers if they register one.
      relay(relayChannel, '', ev.data, ev.lastEventId || null);
    };

    // The server's heartbeat event has no payload — listening for it here
    // (in addition to any data events) is what keeps the watchdog alive
    // during quiet periods.
    es.addEventListener('heartbeat', markEvent);

    if (opts.listeners) {
      for (const name of Object.keys(opts.listeners)) {
        es.addEventListener(name, ((ev: MessageEvent) => {
          retryDelay = opts.initialDelay ?? 1000;
          markEvent();
          let data: unknown = ev.data;
          try { data = JSON.parse(ev.data as string); } catch { /* keep raw */ }
          dispatch(name, data, ev.lastEventId);
          // Relay the already-parsed object so followers run the identical
          // handler with no second parse.
          relay(relayChannel, name, data, ev.lastEventId || null);
        }) as EventListener);
      }
    }

    es.onerror = () => {
      connected.value = false;
      connState.value = 'reconnecting';
      es?.close();
      es = null;
      if (!stopped) {
        const jitter = retryDelay * (0.5 + Math.random());
        retryTimer = setTimeout(connectLeader, jitter);
        retryDelay = Math.min(retryDelay * 2, maxDelay);
      }
    };
  }

  // Follower path: no EventSource. Subscribe to relayed events and run the same
  // listeners the leader runs. Followers report 'ok' — relayed events flowing
  // through the leader is the live signal we have.
  function connectFollower() {
    teardownFollower();
    connected.value = true;
    connState.value = 'ok';
    if (opts.listeners) {
      for (const name of Object.keys(opts.listeners)) {
        followerUnsubs.push(
          subscribeAsFollower(relayChannel, name, (data, lastEventId) => {
            dispatch(name, data, lastEventId);
          }),
        );
      }
    }
    if (opts.onMessage) {
      followerUnsubs.push(
        subscribeAsFollower(relayChannel, '', (data, lastEventId) => {
          opts.onMessage?.({ data, lastEventId: lastEventId ?? '' } as MessageEvent);
        }),
      );
    }
    // The leader's initial `snapshot` already fired on its own connection, so a
    // late-opening follower's relay subscription would miss it and render an
    // empty store. Seed the follower's source-of-truth once via the same
    // recovery callback the watchdog uses; live deltas then keep it current.
    opts.onStaleRestart?.();
  }

  function teardownFollower() {
    for (const u of followerUnsubs) u();
    followerUnsubs = [];
  }

  function teardownLeader() {
    if (retryTimer) { clearTimeout(retryTimer); retryTimer = null; }
    es?.close();
    es = null;
  }

  // Pick the right transport for the current leadership state.
  function activate() {
    if (stopped) return;
    if (isLeader()) {
      teardownFollower();
      connectLeader();
    } else {
      teardownLeader();
      connectFollower();
    }
  }

  function stop() {
    stopped = true;
    teardownLeader();
    teardownFollower();
    if (watchdog) clearInterval(watchdog);
    connected.value = false;
    connState.value = 'closed';
  }

  // Re-activate on leadership transitions: a promoted follower opens the real
  // stream; a demoted leader (rare) drops to relay.
  const offLeadership = onLeadershipChange(() => activate());

  activate();
  if (staleMs > 0 && typeof setInterval === 'function') {
    watchdog = setInterval(() => {
      // Watchdog only matters for the leader's real connection.
      if (!isLeader() || !lastEventAt || stopped) return;
      if (Date.now() - lastEventAt > staleMs) {
        // Reset so we don't restart again before the next event arrives.
        lastEventAt = Date.now();
        restartStale();
      }
    }, stalenessIntervalMs);
  }
  onUnmounted(() => {
    offLeadership();
    stop();
  });

  return { connected, connState, stop };
}
