// --- Raw/Preview toggle helpers ---
//
// Shared toggle logic for switching between rendered markdown and raw text.
// Replaces identical patterns in modal-results.js, markdown.js.

/**
 * Toggle between a rendered element and a raw element, updating a button label.
 * Convention: when raw is hidden, button says "Raw"; when raw is shown, "Preview".
 *
 * @param {HTMLElement} renderedEl  The rendered/preview element.
 * @param {HTMLElement} rawEl       The raw text element.
 * @param {HTMLElement} [btn]       The toggle button (updated with "Raw"/"Preview").
 */
function toggleRenderedRaw(renderedEl, rawEl, btn) {
  if (!renderedEl || !rawEl) return;
  var showingRaw = !rawEl.classList.contains("hidden");
  if (showingRaw) {
    renderedEl.classList.remove("hidden");
    rawEl.classList.add("hidden");
    if (btn) btn.textContent = "Raw";
  } else {
    renderedEl.classList.add("hidden");
    rawEl.classList.remove("hidden");
    if (btn) btn.textContent = "Preview";
  }
}
