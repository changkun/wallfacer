// --- Shared formatting utilities ---
//
// Common text formatting and escaping functions used across the UI.
// escapeHtml was previously in utils.js; _fmtMs was in modal-results.js.

/** Escape a string for safe HTML embedding. */
function escapeHtml(s: string | null | undefined): string {
  if (!s) return "";
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

/** Format a millisecond duration as a human-readable string. */
function fmtMs(ms: number): string {
  if (ms < 1000) return ms + "ms";
  if (ms < 60000) return (ms / 1000).toFixed(1) + "s";
  const m = Math.floor(ms / 60000);
  const s = Math.round((ms % 60000) / 1000);
  return m + "m\u202f" + s + "s";
}

/** Format a relative time string (e.g. "3m ago", "2h ago"). */
function timeAgo(dateStr: string): string {
  const d = new Date(dateStr);
  const s = Math.floor((Date.now() - d.getTime()) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return Math.floor(s / 60) + "m ago";
  if (s < 86400) return Math.floor(s / 3600) + "h ago";
  return Math.floor(s / 86400) + "d ago";
}

/** Format a timeout value in minutes as a compact string (e.g. "5m", "1h30m"). */
function formatTimeout(minutes: number): string {
  if (!minutes) return "5m";
  if (minutes < 60) return minutes + "m";
  if (minutes % 60 === 0) return minutes / 60 + "h";
  return Math.floor(minutes / 60) + "h" + (minutes % 60) + "m";
}
