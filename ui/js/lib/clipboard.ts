// --- Clipboard helpers ---
//
// Shared copy-to-clipboard with visual button feedback.
// Replaces identical patterns in modal-results.js, markdown.js.

/**
 * Copy text to the clipboard and show temporary feedback on a button element.
 */
function copyWithFeedback(
  text: string,
  btn: HTMLElement | null,
  feedback?: string,
  duration?: number,
): void {
  if (!btn) return;
  navigator.clipboard
    .writeText(text)
    .then(() => {
      const origHTML = btn.innerHTML;
      btn.textContent = feedback || "Copied!";
      setTimeout(() => {
        btn.innerHTML = origHTML;
      }, duration || 1500);
    })
    .catch(() => {});
}
