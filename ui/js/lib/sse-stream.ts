// --- Managed EventSource with jittered exponential backoff ---
//
// Shared reconnect wrapper for SSE streams that want:
//   - URL wrapped with withAuthToken
//   - Exponential backoff on disconnect, capped and jittered
//   - Backoff reset when a message is received (not when we reconnect)
//   - Single stop() that disables further auto-reconnect
//
// Used by explorer.js, spec-explorer.js, and git.js (leader path).
// api.js (tasks stream) and images.js (pull progress) keep bespoke logic
// because they carry resume IDs and per-pull lifetimes.

declare function withAuthToken(url: string): string;

/** Handle returned by createSSEStream. */
interface SSEStreamHandle {
  /** Close and disable auto-reconnect. */
  stop(): void;
}

interface SSEStreamOptions {
  /** Target URL; will be wrapped with withAuthToken. */
  url: string;
  /** Handler for the default "message" event. */
  onMessage?: (e: MessageEvent) => void;
  /** Named-event handlers. */
  listeners?: Record<string, (e: MessageEvent) => void>;
  /** Called after each EventSource is constructed (successful or not). */
  onCreate?: (source: EventSource) => void;
  /** First retry delay in ms. Defaults to 1000. */
  initialDelay?: number;
  /** Cap for exponential backoff in ms. Defaults to 30000. */
  maxDelay?: number;
}

function createSSEStream(opts: SSEStreamOptions): SSEStreamHandle {
  const initialDelay = opts.initialDelay || 1000;
  const maxDelay = opts.maxDelay || 30000;
  let retryDelay = initialDelay;
  let source: EventSource | null = null;
  let retryTimer: ReturnType<typeof setTimeout> | null = null;
  let stopped = false;

  function wrapReset(
    fn: (e: MessageEvent) => void,
  ): (e: MessageEvent) => void {
    return function (e: MessageEvent): void {
      retryDelay = initialDelay;
      fn(e);
    };
  }

  function connect(): void {
    if (stopped) return;
    source = new EventSource(withAuthToken(opts.url));
    if (opts.onCreate) opts.onCreate(source);

    const es = source;
    if (opts.onMessage) es.onmessage = wrapReset(opts.onMessage);
    if (opts.listeners) {
      const listeners = opts.listeners;
      Object.keys(listeners).forEach(function (name) {
        es.addEventListener(name, wrapReset(listeners[name]));
      });
    }

    source.onerror = function (): void {
      if (!source || source.readyState !== EventSource.CLOSED) return;
      source = null;
      if (stopped) return;
      const jittered = retryDelay * (1 + Math.random()); // uniform [base, 2×base]
      retryTimer = setTimeout(connect, jittered);
      retryDelay = Math.min(retryDelay * 2, maxDelay);
    };
  }

  connect();

  return {
    stop: function (): void {
      stopped = true;
      if (retryTimer) {
        clearTimeout(retryTimer);
        retryTimer = null;
      }
      if (source) {
        source.close();
        source = null;
      }
    },
  };
}
