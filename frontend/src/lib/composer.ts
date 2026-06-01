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

// A flow accepts an empty prompt when it is the built-in "brainstorm" or any
// flow whose spawn_kind is "idea-agent" — the agent builds its own prompt from
// workspace signals. Mirrors the server rule on POST /api/tasks and
// ui/js/tasks.js _flowAllowsEmptyPrompt.
export function flowAllowsEmptyPrompt(
  slug: string,
  flows: ReadonlyArray<{ slug: string; spawn_kind?: string }>,
): boolean {
  if (slug === 'brainstorm') return true;
  const f = flows.find((x) => x.slug === slug);
  return !!(f && f.spawn_kind === 'idea-agent');
}

// Split a multi-paragraph prompt into separate task prompts. Paragraphs are
// blank-line-separated; per-paragraph leading / trailing whitespace is
// trimmed and empty paragraphs are dropped. Caller is expected to cap the
// list at 50 to match the server-side /api/tasks/batch limit.
export function splitBatch(input: string): string[] {
  if (!input) return [];
  return input
    .split(/\n\s*\n+/)
    .map((p) => p.trim())
    .filter(Boolean);
}
