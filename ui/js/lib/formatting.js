// --- Shared formatting utilities ---
//
// Common text formatting and escaping functions used across the UI.
// escapeHtml was previously in utils.js; _fmtMs was in modal-results.js.

/**
 * Escape a string for safe HTML embedding.
 * @param {string} s
 * @returns {string}
 */
function escapeHtml(s) {
  if (!s) return "";
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

/**
 * Format a millisecond duration as a human-readable string.
 * @param {number} ms
 * @returns {string}
 */
function fmtMs(ms) {
  if (ms < 1000) return ms + "ms";
  if (ms < 60000) return (ms / 1000).toFixed(1) + "s";
  var m = Math.floor(ms / 60000);
  var s = Math.round((ms % 60000) / 1000);
  return m + "m\u202f" + s + "s";
}

/**
 * Format a relative time string (e.g. "3m ago", "2h ago").
 * @param {string} dateStr  ISO date string.
 * @returns {string}
 */
function timeAgo(dateStr) {
  var d = new Date(dateStr);
  var s = Math.floor((Date.now() - d) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return Math.floor(s / 60) + "m ago";
  if (s < 86400) return Math.floor(s / 3600) + "h ago";
  return Math.floor(s / 86400) + "d ago";
}

/**
 * Format a timeout value in minutes as a compact string (e.g. "5m", "1h30m").
 * @param {number} minutes
 * @returns {string}
 */
function formatTimeout(minutes) {
  if (!minutes) return "5m";
  if (minutes < 60) return minutes + "m";
  if (minutes % 60 === 0) return minutes / 60 + "h";
  return Math.floor(minutes / 60) + "h" + (minutes % 60) + "m";
}
