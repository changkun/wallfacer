import { describe, it, expect } from 'vitest';
import { tags } from '@lezer/highlight';
import { consoleHighlight, consoleTheme } from './editorTheme';

// The editor reads the ramp, never a bundled palette: every color in the
// highlight style is a token, and the keyword tone matches styles/syntax.css.
describe('editorTheme', () => {
  it('maps tokens to the ramp with no literal colors', () => {
    const specs = consoleHighlight.specs;
    for (const s of specs) {
      expect(String(s.color)).toMatch(/^(var\(--|color-mix\()/);
    }
    const keyword = specs.find((s) => Array.isArray(s.tag) ? s.tag.includes(tags.keyword) : s.tag === tags.keyword);
    expect(keyword?.color).toBe('var(--err)');
    const comment = specs.find((s) => Array.isArray(s.tag) && s.tag.includes(tags.comment));
    expect(comment?.color).toBe('var(--ink-3)');
  });
  it('builds a light and a dark extension', () => {
    expect(consoleTheme(false)).toBeTruthy();
    expect(consoleTheme(true)).toBeTruthy();
  });
});
