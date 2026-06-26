// Collapse the client-side inline diff-review comments and an optional general
// message into one structured markdown string for the agent. The result is sent
// under the `message` key of POST /api/tasks/{id}/feedback — the same channel the
// Overview feedback textarea uses — so the runner consumes it as the next turn's
// prompt with enough context to locate each commented line.
import type { DiffComment } from '../stores/diffComments';

// formatBatchFeedback groups line comments by file (first-seen order) under an
// "Inline Review Comments" section and appends the general feedback. Either
// section is omitted when empty; an empty input yields an empty string (the
// caller disables submit in that case).
export function formatBatchFeedback(comments: DiffComment[], general: string): string {
  const sections: string[] = [];

  if (comments.length > 0) {
    const order: string[] = [];
    const byFile = new Map<string, DiffComment[]>();
    for (const c of comments) {
      let bucket = byFile.get(c.filename);
      if (!bucket) {
        bucket = [];
        byFile.set(c.filename, bucket);
        order.push(c.filename);
      }
      bucket.push(c);
    }

    const parts: string[] = ['## Inline Review Comments'];
    for (const filename of order) {
      parts.push(`### ${filename}`);
      for (const c of byFile.get(filename)!) {
        // Adds/context reference the new-file line; deletions the old-file line.
        const lineNo = c.newLine ?? c.oldLine;
        parts.push(`**Line ${lineNo}** (\`${c.lineText}\`):\n${c.body}`);
      }
    }
    sections.push(parts.join('\n\n'));
  }

  const g = general.trim();
  if (g) sections.push(`## General Feedback\n\n${g}`);

  return sections.join('\n\n');
}
