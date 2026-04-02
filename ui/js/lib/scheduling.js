// --- Render scheduling helpers ---
//
// Factory for requestAnimationFrame-debounced render schedulers.
// Replaces identical patterns in modal-logs.js, refine.js, render.js.

/**
 * Create a scheduler that coalesces rapid calls into a single
 * requestAnimationFrame callback. Useful for batching DOM updates
 * triggered by high-frequency events (SSE chunks, input events, etc.).
 *
 * @param {function} callback  The function to call at most once per frame.
 * @returns {function}         The debounced schedule function.
 */
function createRAFScheduler(callback) {
  var pending = false;
  return function schedule() {
    if (pending) return;
    pending = true;
    requestAnimationFrame(function () {
      pending = false;
      callback();
    });
  };
}
