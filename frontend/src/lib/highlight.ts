// Escapes text for safe HTML embedding and wraps the first occurrence of
// query with a <mark> element for visual highlighting. Mirrors the legacy
// ui/js/search.js highlightMatch helper for board-filter parity.

export function escapeHtml(s: string): string {
  return String(s ?? '')
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

export function highlightMatch(text: string, query: string): string {
  if (!query || !text) return escapeHtml(text);
  const idx = text.toLowerCase().indexOf(query.toLowerCase());
  if (idx === -1) return escapeHtml(text);
  return (
    escapeHtml(text.slice(0, idx)) +
    '<mark class="search-highlight">' +
    escapeHtml(text.slice(idx, idx + query.length)) +
    '</mark>' +
    escapeHtml(text.slice(idx + query.length))
  );
}
