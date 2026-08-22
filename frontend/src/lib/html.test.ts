import { describe, it, expect } from 'vitest';
import { escapeHtml } from './html';

describe('escapeHtml', () => {
  it('escapes every character that can break out of markup', () => {
    expect(escapeHtml(`<b>"a&b"</b>`)).toBe('&lt;b&gt;&quot;a&amp;b&quot;&lt;/b&gt;');
    expect(escapeHtml("it's")).toBe('it&#39;s');
  });

  it('escapes the ampersand first so entities are not double-built', () => {
    expect(escapeHtml('&lt;')).toBe('&amp;lt;');
  });

  // The ansi.ts copy escaped neither quote. Its output only ever landed in text
  // content, so nothing broke, but a caller reusing it for an attribute would
  // have had a hole. Pinning both quotes keeps the shared helper attribute-safe.
  it('escapes quotes so the result is safe inside an attribute value', () => {
    const attr = `<img title="${escapeHtml('" onerror="alert(1)')}">`;
    expect(attr).not.toMatch(/onerror="/);
  });

  it('is null-safe because callers pass values straight off the wire', () => {
    expect(escapeHtml(null as unknown as string)).toBe('');
    expect(escapeHtml(undefined as unknown as string)).toBe('');
  });
});
