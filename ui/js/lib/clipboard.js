"use strict";
function copyWithFeedback(text, btn, feedback, duration) {
  if (!btn) return;
  navigator.clipboard.writeText(text).then(() => {
    const origHTML = btn.innerHTML;
    btn.textContent = feedback || "Copied!";
    setTimeout(() => {
      btn.innerHTML = origHTML;
    }, duration || 1500);
  }).catch(() => {
  });
}
