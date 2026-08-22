// Escapes the five characters that can break out of HTML text or an attribute
// value. Single source of truth: three copies had drifted, and the weakest of
// them left `"` unescaped, which is safe in text content but not in an
// attribute — a distinction the next caller should not have to rediscover.
//
// Null-safe because callers pass values straight off the wire.
export function escapeHtml(s: string): string {
  return String(s ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}
