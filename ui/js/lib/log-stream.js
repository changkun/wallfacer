// --- Streaming fetch helpers ---
//
// Shared streaming fetch + chunk accumulation for log panels.
// Replaces the common streaming pattern in modal-logs.js.

/**
 * Start a streaming fetch that decodes chunks and calls onChunk for each.
 * Handles AbortController lifecycle and reconnect scheduling.
 *
 * @param {Object} opts
 * @param {string}   opts.url             Fetch URL (will be wrapped with auth token).
 * @param {AbortSignal} [opts.signal]     External abort signal.
 * @param {function(string)} opts.onChunk Called with each decoded text chunk.
 * @param {function()}  [opts.onDone]     Called when the stream ends normally.
 * @param {function()}  [opts.onError]    Called on non-abort errors.
 * @param {function()}  [opts.isStale]    Return true to stop reading (e.g. modal closed).
 * @param {function()}  [opts.onFirstChunk] Called once before the first onChunk.
 * @returns {{abort: function}}           Handle to abort the stream.
 */
function startStreamingFetch(opts) {
  var abortController = new AbortController();
  var decoder = new TextDecoder();
  var receivedData = false;

  // Merge external signal if provided.
  var signal = abortController.signal;
  if (opts.signal) {
    opts.signal.addEventListener("abort", function () {
      abortController.abort();
    });
  }

  fetch(withAuthToken(opts.url), {
    signal: signal,
    headers: withBearerHeaders(),
  })
    .then(function (res) {
      if (!res.ok || !res.body) {
        if (opts.onError) opts.onError();
        return;
      }
      var reader = res.body.getReader();
      function read() {
        reader
          .read()
          .then(function (result) {
            if (opts.isStale && opts.isStale()) return;
            if (result.done) {
              if (opts.onDone) opts.onDone(receivedData);
              return;
            }
            if (!receivedData) {
              receivedData = true;
              if (opts.onFirstChunk) opts.onFirstChunk();
            }
            opts.onChunk(decoder.decode(result.value, { stream: true }));
            read();
          })
          .catch(function () {
            if (opts.onError) opts.onError();
          });
      }
      read();
    })
    .catch(function (err) {
      if (err.name === "AbortError") return;
      if (opts.onError) opts.onError();
    });

  return {
    abort: function () {
      abortController.abort();
    },
  };
}
