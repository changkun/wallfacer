// --- Clipboard helpers ---
//
// Shared copy-to-clipboard with visual button feedback.
// Replaces identical patterns in modal-results.js, markdown.js.

/**
 * Copy text to the clipboard and show temporary feedback on a button element.
 * @param {string} text        The text to copy.
 * @param {HTMLElement} btn    The button to show feedback on.
 * @param {string} [feedback]  Feedback text (default "Copied!").
 * @param {number} [duration]  Feedback duration in ms (default 1500).
 */
function copyWithFeedback(text, btn, feedback, duration) {
  if (!btn) return;
  navigator.clipboard
    .writeText(text)
    .then(function () {
      var origHTML = btn.innerHTML;
      btn.textContent = feedback || "Copied!";
      setTimeout(function () {
        btn.innerHTML = origHTML;
      }, duration || 1500);
    })
    .catch(function () {});
}
