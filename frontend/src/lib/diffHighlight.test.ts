import { describe, it, expect } from 'vitest';
import { extToLang, splitHighlightedLines } from './diffHighlight';

describe('extToLang', () => {
  it('maps common extensions', () => {
    expect(extToLang('src/app.ts')).toBe('typescript');
    expect(extToLang('main.go')).toBe('go');
    expect(extToLang('style.scss')).toBe('scss');
  });
  it('maps special filenames', () => {
    expect(extToLang('Dockerfile')).toBe('dockerfile');
    expect(extToLang('path/Makefile')).toBe('makefile');
  });
  it('returns null for unknown extensions', () => {
    expect(extToLang('mystery.xyz')).toBeNull();
    expect(extToLang('noext')).toBeNull();
  });
});

describe('splitHighlightedLines', () => {
  it('splits plain newlines into separate lines', () => {
    expect(splitHighlightedLines('a\nb\nc')).toEqual(['a', 'b', 'c']);
  });
  it('closes and reopens spans across line boundaries', () => {
    const out = splitHighlightedLines('<span class="x">a\nb</span>');
    expect(out).toEqual(['<span class="x">a</span>', '<span class="x">b</span>']);
  });
  it('preserves non-span tags within a line', () => {
    expect(splitHighlightedLines('x<br>y')).toEqual(['x<br>y']);
  });
});
