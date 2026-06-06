// Chunked-text fetch reader. Mirrors the original ui/js/transport.js
// startStreamingFetch helper: reads the response body as text chunks,
// invokes onChunk for each, and reports whether any data arrived in onDone.
// Used by the planning chat where the server emits raw NDJSON over plain
// HTTP (not SSE), so EventSource cannot be used.

export interface StreamingFetchOptions {
  url: string;
  onChunk: (chunk: string) => void;
  onDone?: (hadData: boolean) => void;
  onError?: (err: unknown) => void;
}

export interface StreamingFetchHandle {
  abort: () => void;
}

import { authHeaders } from '../api/client';

export function startStreamingFetch(opts: StreamingFetchOptions): StreamingFetchHandle {
  const ctrl = new AbortController();
  let aborted = false;
  let hadData = false;

  (async () => {
    try {
      const res = await fetch(opts.url, {
        method: 'GET',
        credentials: 'same-origin',
        headers: { Accept: 'text/plain', ...authHeaders() },
        signal: ctrl.signal,
      });

      // 204 No Content: no exec in flight.
      if (res.status === 204 || !res.body) {
        opts.onDone?.(false);
        return;
      }
      if (!res.ok) {
        opts.onError?.(new Error(`HTTP ${res.status}`));
        return;
      }

      const reader = res.body.getReader();
      const decoder = new TextDecoder();

      while (!aborted) {
        const { value, done } = await reader.read();
        if (done) break;
        if (value && value.length > 0) {
          hadData = true;
          opts.onChunk(decoder.decode(value, { stream: true }));
        }
      }
      const tail = decoder.decode();
      if (tail) {
        hadData = true;
        opts.onChunk(tail);
      }
      opts.onDone?.(hadData);
    } catch (err) {
      if (aborted) return;
      opts.onError?.(err);
    }
  })();

  return {
    abort() {
      aborted = true;
      ctrl.abort();
    },
  };
}
