import { describe, it, expect } from 'vitest';
import { highlightMatch, escapeHtml } from './highlight';

describe('escapeHtml', () => {
  it('escapes angle brackets, ampersands, and quotes', () => {
    expect(escapeHtml('<b>"a&b"</b>')).toBe('&lt;b&gt;&quot;a&amp;b&quot;&lt;/b&gt;');
  });
  it('handles null/undefined', () => {
    expect(escapeHtml(null as unknown as string)).toBe('');
    expect(escapeHtml(undefined as unknown as string)).toBe('');
  });
});

describe('highlightMatch', () => {
  it('returns escaped text unchanged when query is empty', () => {
    expect(highlightMatch('Hello <world>', '')).toBe('Hello &lt;world&gt;');
  });
  it('returns escaped text unchanged when query is null/undefined', () => {
    expect(highlightMatch('Hello World', null as unknown as string)).toBe('Hello World');
    expect(highlightMatch('Hello World', undefined as unknown as string)).toBe('Hello World');
  });
  it('wraps the matching substring in <mark class="search-highlight">', () => {
    expect(highlightMatch('Hello World', 'World')).toBe('Hello <mark class="search-highlight">World</mark>');
  });
  it('is case-insensitive and preserves original casing', () => {
    expect(highlightMatch('Hello World', 'world')).toBe('Hello <mark class="search-highlight">World</mark>');
  });
  it('escapes HTML in surrounding text', () => {
    expect(highlightMatch('<b>match</b>', 'match')).toBe(
      '&lt;b&gt;<mark class="search-highlight">match</mark>&lt;/b&gt;',
    );
  });
  it('returns escaped text when no match found', () => {
    expect(highlightMatch('Hello World', 'xyz')).toBe('Hello World');
  });
});
