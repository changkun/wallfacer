import { describe, it, expect } from 'vitest';
import { splitBatch } from './composer';

describe('splitBatch', () => {
  it('returns [] for empty or whitespace input', () => {
    expect(splitBatch('')).toEqual([]);
    expect(splitBatch('   \n\n  \n')).toEqual([]);
  });
  it('treats a single paragraph as one task', () => {
    expect(splitBatch('only one task')).toEqual(['only one task']);
  });
  it('splits on blank-line separators and trims', () => {
    expect(splitBatch('first\n\nsecond\n\nthird')).toEqual(['first', 'second', 'third']);
  });
  it('handles multi-line paragraphs without breaking them', () => {
    expect(splitBatch('a\nb\n\nc\nd')).toEqual(['a\nb', 'c\nd']);
  });
  it('collapses multiple blank lines into one separator', () => {
    expect(splitBatch('a\n\n\n\nb')).toEqual(['a', 'b']);
  });
});
