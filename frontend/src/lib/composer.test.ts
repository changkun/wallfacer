import { describe, it, expect } from 'vitest';
import { parseTags } from './composer';

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
