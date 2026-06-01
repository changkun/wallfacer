import { describe, it, expect } from 'vitest';
import { parseTags, splitBatch, flowAllowsEmptyPrompt } from './composer';

describe('flowAllowsEmptyPrompt', () => {
  const flows = [
    { slug: 'implement' },
    { slug: 'brainstorm' },
    { slug: 'ideate', spawn_kind: 'idea-agent' },
  ];
  it('allows the built-in brainstorm flow', () => {
    expect(flowAllowsEmptyPrompt('brainstorm', flows)).toBe(true);
  });
  it('allows idea-agent spawn-kind flows', () => {
    expect(flowAllowsEmptyPrompt('ideate', flows)).toBe(true);
  });
  it('requires a prompt for every other flow', () => {
    expect(flowAllowsEmptyPrompt('implement', flows)).toBe(false);
    expect(flowAllowsEmptyPrompt('unknown', flows)).toBe(false);
  });
});

describe('parseTags', () => {
  it('returns [] for empty/whitespace', () => {
    expect(parseTags('')).toEqual([]);
    expect(parseTags('  ,  , ')).toEqual([]);
  });
  it('splits on commas and trims', () => {
    expect(parseTags('a, b ,c')).toEqual(['a', 'b', 'c']);
  });
  it('drops blanks and de-duplicates preserving order', () => {
    expect(parseTags('x, , x, y, x')).toEqual(['x', 'y']);
  });
});

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
