import { ref, onUnmounted } from 'vue';

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
  let es: EventSource | null = null;
  let retryDelay = opts.initialDelay ?? 1000;
  const maxDelay = opts.maxDelay ?? 30000;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let stopped = false;

  const staleMs = opts.staleThresholdMs ?? 35000;
  const stalenessIntervalMs = opts.stalenessCheckIntervalMs ?? 10000;
  let watchdog: ReturnType<typeof setInterval> | null = null;
  let lastEventAt = 0;

  function addAuthParam(url: string): string {
    if (typeof window !== 'undefined' && window.__WALLFACER__?.serverApiKey) {
      const sep = url.includes('?') ? '&' : '?';
      return url + sep + 'token=' + encodeURIComponent(window.__WALLFACER__.serverApiKey);
    }
    return url;
  }

  function markEvent() {
    lastEventAt = Date.now();
  }

  function restartStale() {
    es?.close();
    es = null;
    connected.value = false;
    opts.onStaleRestart?.();
    connect();
  }

  function connect() {
    if (stopped) return;
    es = new EventSource(addAuthParam(opts.url), {
      withCredentials: opts.withCredentials ?? true,
    });

    es.onopen = () => {
      connected.value = true;
      retryDelay = opts.initialDelay ?? 1000;
      markEvent();
    };

    es.onmessage = (ev) => {
      retryDelay = opts.initialDelay ?? 1000;
      markEvent();
      opts.onMessage?.(ev);
    };

    // The server's heartbeat event has no payload — listening for it here
    // (in addition to any data events) is what keeps the watchdog alive
    // during quiet periods.
    es.addEventListener('heartbeat', markEvent);

    if (opts.listeners) {
      for (const [name, handler] of Object.entries(opts.listeners)) {
        es.addEventListener(name, ((ev: MessageEvent) => {
          retryDelay = opts.initialDelay ?? 1000;
          markEvent();
          let data: unknown = ev.data;
          try { data = JSON.parse(ev.data as string); } catch { /* keep raw */ }
          handler(data, ev.lastEventId);
        }) as EventListener);
      }
    }

    es.onerror = () => {
      connected.value = false;
      es?.close();
      es = null;
      if (!stopped) {
        const jitter = retryDelay * (0.5 + Math.random());
        retryTimer = setTimeout(connect, jitter);
        retryDelay = Math.min(retryDelay * 2, maxDelay);
      }
    };
  }

  function stop() {
    stopped = true;
    if (retryTimer) clearTimeout(retryTimer);
    if (watchdog) clearInterval(watchdog);
    es?.close();
    es = null;
    connected.value = false;
  }

  connect();
  if (staleMs > 0 && typeof setInterval === 'function') {
    watchdog = setInterval(() => {
      if (!lastEventAt || stopped) return;
      if (Date.now() - lastEventAt > staleMs) {
        // Reset so we don't restart again before the next event arrives.
        lastEventAt = Date.now();
        restartStale();
      }
    }, stalenessIntervalMs);
  }
  onUnmounted(stop);

  return { connected, stop };
}
