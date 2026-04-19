// --- Render scheduling helpers ---
//
// Factory for requestAnimationFrame-debounced render schedulers.
// Replaces identical patterns in modal-logs.js, render.js.

/**
 * Create a scheduler that coalesces rapid calls into a single
 * requestAnimationFrame callback. Useful for batching DOM updates
 * triggered by high-frequency events (SSE chunks, input events, etc.).
 */
function createRAFScheduler(callback: () => void): () => void {
  let pending = false;
  return function schedule(): void {
    if (pending) return;
    pending = true;
    requestAnimationFrame(() => {
      pending = false;
      callback();
    });
  };
}
