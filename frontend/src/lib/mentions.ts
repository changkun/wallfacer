// Pure helpers for @-mention file autocomplete (ports ui/js/mention.js's query
// detection + filtering). The reactive dropdown/keyboard lives in the
// useMentions composable; the parsing logic is here so it's unit-testable.

export interface MentionQuery {
  query: string;
  atIdx: number;
}

// Returns the active "@mention" query when the caret (caretPos) sits inside one,
// else null. The "@" must be at the start of the text or preceded by whitespace,
// and the query must not contain whitespace.
export function mentionQueryAt(text: string, caretPos: number): MentionQuery | null {
  const before = text.slice(0, caretPos);
  const atIdx = before.lastIndexOf('@');
  if (atIdx === -1) return null;
  if (atIdx > 0 && !/\s/.test(before[atIdx - 1])) return null;
  const query = before.slice(atIdx + 1);
  if (/\s/.test(query)) return null;
  return { query, atIdx };
}

// Filter+rank files for a query. Files whose basename starts with the query
// rank first, then path substring matches; a priorityPrefix (e.g. "spec/")
// floats matching files to the top. Case-insensitive. Capped at `limit`.
export function filterMentionFiles(
  files: string[],
  query: string,
  priorityPrefix = '',
  limit = 20,
): string[] {
  const q = (query || '').toLowerCase();
  const scored: { f: string; score: number }[] = [];
  for (const f of files) {
    const lower = f.toLowerCase();
    const base = lower.split('/').pop() || lower;
    let score: number;
    if (!q) score = 0;
    else if (base.startsWith(q)) score = 3;
    else if (base.includes(q)) score = 2;
    else if (lower.includes(q)) score = 1;
    else continue;
    if (priorityPrefix && lower.startsWith(priorityPrefix.toLowerCase())) score += 4;
    scored.push({ f, score });
  }
  scored.sort((a, b) => (b.score - a.score) || a.f.localeCompare(b.f));
  return scored.slice(0, limit).map((s) => s.f);
}

// Build the replacement when a file is chosen: replaces the "@query" span at
// atIdx (up to caretPos) with "@file ". Returns the new text + caret position.
export function applyMention(
  text: string,
  atIdx: number,
  caretPos: number,
  file: string,
): { text: string; caret: number } {
  const before = text.slice(0, atIdx);
  const after = text.slice(caretPos);
  const insert = `@${file} `;
  return { text: before + insert + after, caret: before.length + insert.length };
}
