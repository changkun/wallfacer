// Board search/filter matching, ported from ui/js/search.js's matchesFilter.
//
// The query is whitespace-tokenized. Tokens starting with "#" are tag
// filters — EVERY tag token must be present (exact, case-insensitive) in the
// task's tags. The remaining text tokens must EACH be a substring of the
// task's title, prompt, joined tags, or a prefix of its id. All conditions
// are AND-ed, so `#bug login timeout` means: tagged "bug" AND title/prompt/
// tags contain "login" AND contain "timeout". An empty query matches all.

export interface FilterableTask {
  id: string;
  title?: string;
  prompt?: string;
  tags?: string[];
}

export function matchesTaskFilter(t: FilterableTask, query: string): boolean {
  const q = (query || '').trim().toLowerCase();
  if (!q) return true;

  const tokens = q.split(/\s+/).filter(Boolean);
  const tagTokens = tokens
    .filter((tok) => tok.startsWith('#'))
    .map((tok) => tok.slice(1))
    .filter(Boolean);
  const textTokens = tokens.filter((tok) => !tok.startsWith('#'));

  const taskTags = (t.tags || []).map((tag) => String(tag).toLowerCase());

  // Every #tag token must match a tag exactly.
  for (const tagTok of tagTokens) {
    if (!taskTags.includes(tagTok)) return false;
  }

  // A bare "#" with no text tokens (e.g. query was just "#") matches nothing
  // useful; legacy treated it as no tag constraint. With at least one tag
  // token and no text tokens, the tag check above is the whole predicate.
  if (textTokens.length === 0) return true;

  const title = (t.title || '').toLowerCase();
  const prompt = (t.prompt || '').toLowerCase();
  const tagText = taskTags.join(' ');
  const id = (t.id || '').toLowerCase();

  // Every text token must match somewhere.
  for (const tok of textTokens) {
    if (
      !title.includes(tok) &&
      !prompt.includes(tok) &&
      !tagText.includes(tok) &&
      !id.startsWith(tok)
    ) {
      return false;
    }
  }
  return true;
}
