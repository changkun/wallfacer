import { describe, it, expect } from 'vitest';
import { mentionQueryAt, filterMentionFiles, applyMention } from './mentions';

describe('mentionQueryAt', () => {
  it('detects @query at caret', () => {
    expect(mentionQueryAt('see @rea', 8)).toEqual({ query: 'rea', atIdx: 4 });
  });
  it('detects @ at start of text', () => {
    expect(mentionQueryAt('@foo', 4)).toEqual({ query: 'foo', atIdx: 0 });
  });
  it('returns null when @ is not preceded by whitespace', () => {
    expect(mentionQueryAt('email@x', 7)).toBeNull();
  });
  it('returns null when the query contains whitespace (mention ended)', () => {
    expect(mentionQueryAt('@foo bar', 8)).toBeNull();
  });
  it('returns null without an @', () => {
    expect(mentionQueryAt('hello', 5)).toBeNull();
  });
});

describe('filterMentionFiles', () => {
  const files = ['src/README.md', 'src/app.js', 'docs/readme-extra.md', 'specs/plan.md'];
  it('empty query returns all (capped)', () => {
    expect(filterMentionFiles(files, '')).toHaveLength(4);
  });
  it('ranks basename-prefix matches above path-substring matches', () => {
    const data = ['src/app.js', 'docs/the-app.md', 'lib/readme.md'];
    const r = filterMentionFiles(data, 'app');
    expect(r[0]).toBe('src/app.js'); // basename "app.js" starts with "app" (score 3)
    expect(r).toContain('docs/the-app.md'); // basename substring (score 2)
    expect(r).not.toContain('lib/readme.md');
  });
  it('priorityPrefix floats matching files up', () => {
    const r = filterMentionFiles(files, '', 'specs/');
    expect(r[0]).toBe('specs/plan.md');
  });
  it('priorityPrefix matches the prefix anywhere, not just at path start', () => {
    // Real paths carry a workspace-basename prefix (e.g. "repo/specs/...").
    const data = ['repo/src/app.js', 'repo/specs/plan.md'];
    const r = filterMentionFiles(data, '', 'specs/');
    expect(r[0]).toBe('repo/specs/plan.md');
  });
  it('respects the limit', () => {
    expect(filterMentionFiles(files, '', '', 2)).toHaveLength(2);
  });
});

describe('applyMention', () => {
  it('replaces the @query span with "@file "', () => {
    // "see @rea" caret at 8, choose src/README.md
    const { text, caret } = applyMention('see @rea', 4, 8, 'src/README.md');
    expect(text).toBe('see @src/README.md ');
    expect(caret).toBe(text.length);
  });
  it('preserves text after the caret', () => {
    const { text } = applyMention('a @r end', 2, 4, 'x.md');
    expect(text).toBe('a @x.md  end');
  });
});
