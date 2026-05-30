// Helpers for the task composer. Kept pure + tested so the parsing logic is
// not buried in the component.

// Parse a free-text tag field into a clean tag list: split on commas, trim,
// drop blanks, de-duplicate (case-sensitive, first occurrence wins).
export function parseTags(input: string): string[] {
  if (!input) return [];
  const seen = new Set<string>();
  const out: string[] = [];
  for (const raw of input.split(',')) {
    const tag = raw.trim();
    if (!tag || seen.has(tag)) continue;
    seen.add(tag);
    out.push(tag);
  }
  return out;
}
