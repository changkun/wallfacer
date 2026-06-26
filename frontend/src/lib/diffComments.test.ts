import { describe, it, expect } from 'vitest';
import { formatBatchFeedback } from './diffComments';
import type { DiffComment } from '../stores/diffComments';

function comment(over: Partial<DiffComment> = {}): DiffComment {
  return {
    id: 'id-' + Math.random().toString(36).slice(2),
    taskId: 't1',
    filename: 'a.go',
    lineIndex: 1,
    oldLine: null,
    newLine: 10,
    kind: 'add',
    lineText: '+added line',
    body: 'use a mutex here',
    ...over,
  };
}

describe('formatBatchFeedback', () => {
  it('returns empty string when there are no comments and no general message', () => {
    expect(formatBatchFeedback([], '')).toBe('');
    expect(formatBatchFeedback([], '   ')).toBe('');
  });

  it('formats line comments only, omitting the general section', () => {
    const out = formatBatchFeedback([comment()], '');
    expect(out).toContain('## Inline Review Comments');
    expect(out).toContain('### a.go');
    expect(out).toContain('**Line 10** (`+added line`):\nuse a mutex here');
    expect(out).not.toContain('## General Feedback');
  });

  it('formats general only, omitting the inline section', () => {
    const out = formatBatchFeedback([], 'looks good overall');
    expect(out).toBe('## General Feedback\n\nlooks good overall');
  });

  it('uses oldLine for deletions and newLine for adds/context', () => {
    const out = formatBatchFeedback(
      [
        comment({ kind: 'del', oldLine: 5, newLine: null, lineText: '-gone', body: 'why removed?' }),
        comment({ kind: 'ctx', oldLine: 6, newLine: 6, lineText: ' kept', body: 'rename this' }),
      ],
      '',
    );
    expect(out).toContain('**Line 5** (`-gone`):\nwhy removed?');
    expect(out).toContain('**Line 6** (` kept`):\nrename this');
  });

  it('groups by file in first-seen order and includes both sections', () => {
    const out = formatBatchFeedback(
      [
        comment({ filename: 'z.go', newLine: 1, lineText: '+z', body: 'first file' }),
        comment({ filename: 'a.go', newLine: 2, lineText: '+a', body: 'second file' }),
        comment({ filename: 'z.go', newLine: 3, lineText: '+z2', body: 'back to z' }),
      ],
      'wrap up',
    );
    // z.go appears before a.go (first-seen), and z.go has both its comments.
    expect(out.indexOf('### z.go')).toBeLessThan(out.indexOf('### a.go'));
    expect(out.indexOf('### z.go')).toBeGreaterThan(-1);
    expect((out.match(/### z\.go/g) || [])).toHaveLength(1);
    expect(out).toContain('first file');
    expect(out).toContain('back to z');
    expect(out.endsWith('## General Feedback\n\nwrap up')).toBe(true);
  });
});
