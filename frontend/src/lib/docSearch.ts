// Ranks doc entries for the command-palette Docs section. Mirrors the legacy
// ui/js/command-palette.js scoring: a title prefix match outranks a title
// substring, which outranks a slug substring; ties break alphabetically by
// title. An empty query returns the entries in their original order.

export interface DocEntry {
  slug: string;
  title: string;
}

export function rankDocs<T extends DocEntry>(entries: readonly T[], query: string, limit = 6): T[] {
  const q = (query || '').trim().toLowerCase();
  if (!q) return entries.slice(0, limit);

  const scored: { entry: T; score: number }[] = [];
  for (const entry of entries) {
    const title = (entry.title || '').toLowerCase();
    const slug = (entry.slug || '').toLowerCase();
    let score = 0;
    if (title.includes(q)) score = title.startsWith(q) ? 3 : 2;
    else if (slug.includes(q)) score = 1;
    if (score > 0) scored.push({ entry, score });
  }
  scored.sort((a, b) =>
    b.score - a.score || (a.entry.title || '').localeCompare(b.entry.title || ''),
  );
  return scored.slice(0, limit).map((s) => s.entry);
}
