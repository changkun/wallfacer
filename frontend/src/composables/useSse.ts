import { ref, onUnmounted } from 'vue';

export interface UseSseOptions {
  url: string;
  withCredentials?: boolean;
  onMessage?: (event: MessageEvent) => void;
  listeners?: Record<string, (data: unknown, lastEventId: string) => void>;
  initialDelay?: number;
  maxDelay?: number;
}

export function useSse(opts: UseSseOptions) {
  const connected = ref(false);
  let es: EventSource | null = null;
  let retryDelay = opts.initialDelay ?? 1000;
  const maxDelay = opts.maxDelay ?? 30000;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let stopped = false;

  function addAuthParam(url: string): string {
    if (typeof window !== 'undefined' && window.__WALLFACER__?.serverApiKey) {
      const sep = url.includes('?') ? '&' : '?';
      return url + sep + 'token=' + encodeURIComponent(window.__WALLFACER__.serverApiKey);
    }
    return url;
  }

  function connect() {
    if (stopped) return;
    es = new EventSource(addAuthParam(opts.url), {
      withCredentials: opts.withCredentials ?? true,
    });

    es.onopen = () => {
      connected.value = true;
      retryDelay = opts.initialDelay ?? 1000;
    };

    es.onmessage = (ev) => {
      retryDelay = opts.initialDelay ?? 1000;
      opts.onMessage?.(ev);
    };

    if (opts.listeners) {
      for (const [name, handler] of Object.entries(opts.listeners)) {
        es.addEventListener(name, ((ev: MessageEvent) => {
          retryDelay = opts.initialDelay ?? 1000;
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
    es?.close();
    es = null;
    connected.value = false;
  }

  connect();
  onUnmounted(stop);

  return { connected, stop };
}
