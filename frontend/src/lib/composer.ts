// Helpers for the task composer. Kept pure + tested so the parsing logic is
// not buried in the component.

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
