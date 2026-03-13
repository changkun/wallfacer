/**
 * Tests for the client-side task search/filter (search.js).
 *
 * matchesFilter and highlightMatch are pure helpers that can be exercised in
 * an isolated vm context without a real browser DOM.
 */
import { describe, it, expect, beforeAll, beforeEach } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

function makeContext(extra = {}) {
  const ctx = {
    document: {
      getElementById: () => null,
      querySelectorAll: () => ({ forEach: () => {} }),
      documentElement: { setAttribute: () => {} },
      readyState: 'complete',
      addEventListener: () => {},
    },
    window: {
      matchMedia: () => ({ matches: false, addEventListener: () => {} }),
    },
    localStorage: {
      getItem: () => null,
      setItem: () => {},
    },
    IntersectionObserver: class {
      constructor() {}
      observe() {}
      unobserve() {}
      disconnect() {}
    },
    Date,
    Math,
    console,
    clearInterval: () => {},
    setInterval: () => 0,
    ...extra,
  };
  return vm.createContext(ctx);
}

function loadScript(filename, ctx) {
  const code = readFileSync(join(jsDir, filename), 'utf8');
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

/** Mutate the filterQuery let-binding inside the vm context. */
function setFilter(ctx, query) {
  vm.runInContext(`filterQuery = ${JSON.stringify(query)};`, ctx);
}

// ---------------------------------------------------------------------------
// matchesFilter
// ---------------------------------------------------------------------------
describe('matchesFilter', () => {
  let ctx;

  beforeAll(() => {
    ctx = makeContext();
    loadScript('state.js', ctx);
    loadScript('utils.js', ctx);
    loadScript('search.js', ctx);
  });

  beforeEach(() => {
    setFilter(ctx, '');
  });

  it('returns true for all tasks when filterQuery is empty', () => {
    expect(ctx.matchesFilter({ title: 'Hello', prompt: 'World' })).toBe(true);
    expect(ctx.matchesFilter({ title: '', prompt: '' })).toBe(true);
    expect(ctx.matchesFilter({})).toBe(true);
  });

  it('(a) hides non-matching cards — returns false when neither title nor prompt matches', () => {
    setFilter(ctx, 'typescript');
    const task = { title: 'Fix login bug', prompt: 'The login form breaks on Safari.' };
    expect(ctx.matchesFilter(task)).toBe(false);
  });

  it('(b) is case-insensitive when matching titles', () => {
    setFilter(ctx, 'hello');
    expect(ctx.matchesFilter({ title: 'Hello World', prompt: 'unrelated' })).toBe(true);
    expect(ctx.matchesFilter({ title: 'HELLO', prompt: 'unrelated' })).toBe(true);
    expect(ctx.matchesFilter({ title: 'say hello', prompt: 'unrelated' })).toBe(true);
  });

  it('(b) is case-insensitive when matching prompts', () => {
    setFilter(ctx, 'IMPLEMENT');
    expect(ctx.matchesFilter({ title: 'Task', prompt: 'implement the feature' })).toBe(true);
    expect(ctx.matchesFilter({ title: 'Task', prompt: 'Implement now' })).toBe(true);
  });

  it('(c) returns true for all tasks after filterQuery is cleared', () => {
    setFilter(ctx, 'specific-term-xyz');
    const task = { title: 'unrelated', prompt: 'unrelated' };
    expect(ctx.matchesFilter(task)).toBe(false);

    setFilter(ctx, '');
    expect(ctx.matchesFilter(task)).toBe(true);
    expect(ctx.matchesFilter({ title: '', prompt: '' })).toBe(true);
  });

  it('(d) matches on title field', () => {
    setFilter(ctx, 'auth');
    expect(ctx.matchesFilter({ title: 'Add authentication', prompt: 'unrelated' })).toBe(true);
    expect(ctx.matchesFilter({ title: 'unrelated', prompt: 'unrelated' })).toBe(false);
  });

  it('(d) matches on prompt field', () => {
    setFilter(ctx, 'dark mode');
    expect(ctx.matchesFilter({ title: 'UI task', prompt: 'Implement dark mode toggle' })).toBe(true);
    expect(ctx.matchesFilter({ title: 'UI task', prompt: 'Light theme only' })).toBe(false);
  });

  it('handles tasks with missing title gracefully', () => {
    setFilter(ctx, 'hello');
    expect(ctx.matchesFilter({ prompt: 'hello world' })).toBe(true);
    expect(ctx.matchesFilter({ title: null, prompt: 'hello world' })).toBe(true);
    expect(ctx.matchesFilter({ title: undefined, prompt: 'no match' })).toBe(false);
  });

  it('handles tasks with missing prompt gracefully', () => {
    setFilter(ctx, 'fix');
    expect(ctx.matchesFilter({ title: 'Fix the bug' })).toBe(true);
    expect(ctx.matchesFilter({ title: 'Add feature' })).toBe(false);
  });

  it('matches tags in free-text search', () => {
    setFilter(ctx, 'backend');
    expect(ctx.matchesFilter({ title: 'unrelated', prompt: 'unrelated', tags: ['backend', 'api'] })).toBe(true);
    expect(ctx.matchesFilter({ title: 'unrelated', prompt: 'unrelated', tags: ['frontend'] })).toBe(false);
  });

  it('handles tasks with no tags gracefully', () => {
    setFilter(ctx, 'backend');
    expect(ctx.matchesFilter({ title: 'unrelated', prompt: 'unrelated' })).toBe(false);
    expect(ctx.matchesFilter({ title: 'unrelated', prompt: 'unrelated', tags: [] })).toBe(false);
  });

  it('#tag token filters by exact tag match', () => {
    setFilter(ctx, '#backend');
    expect(ctx.matchesFilter({ title: 'x', prompt: 'y', tags: ['backend'] })).toBe(true);
    expect(ctx.matchesFilter({ title: 'x', prompt: 'y', tags: ['frontend'] })).toBe(false);
    expect(ctx.matchesFilter({ title: 'x', prompt: 'y' })).toBe(false);
  });

  it('multiple #tag tokens are AND-combined', () => {
    setFilter(ctx, '#backend #api');
    expect(ctx.matchesFilter({ title: 'x', prompt: 'y', tags: ['backend', 'api'] })).toBe(true);
    expect(ctx.matchesFilter({ title: 'x', prompt: 'y', tags: ['backend'] })).toBe(false);
    expect(ctx.matchesFilter({ title: 'x', prompt: 'y', tags: ['api'] })).toBe(false);
  });

  it('#tag + free text both must match', () => {
    setFilter(ctx, '#backend auth');
    expect(ctx.matchesFilter({ title: 'auth service', prompt: 'y', tags: ['backend'] })).toBe(true);
    expect(ctx.matchesFilter({ title: 'auth service', prompt: 'y', tags: ['frontend'] })).toBe(false);
    expect(ctx.matchesFilter({ title: 'unrelated', prompt: 'y', tags: ['backend'] })).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// highlightMatch
// ---------------------------------------------------------------------------
describe('highlightMatch', () => {
  let ctx;

  beforeAll(() => {
    ctx = makeContext();
    loadScript('state.js', ctx);
    loadScript('utils.js', ctx);
    loadScript('search.js', ctx);
  });

  it('returns escaped text unchanged when query is empty string', () => {
    expect(ctx.highlightMatch('Hello <world>', '')).toBe('Hello &lt;world&gt;');
  });

  it('returns escaped text unchanged when query is null/undefined', () => {
    expect(ctx.highlightMatch('Hello World', null)).toBe('Hello World');
    expect(ctx.highlightMatch('Hello World', undefined)).toBe('Hello World');
  });

  it('wraps the matching substring in <mark class="search-highlight">', () => {
    const result = ctx.highlightMatch('Hello World', 'World');
    expect(result).toBe('Hello <mark class="search-highlight">World</mark>');
  });

  it('is case-insensitive: preserves original casing inside the mark', () => {
    const result = ctx.highlightMatch('Hello World', 'world');
    expect(result).toBe('Hello <mark class="search-highlight">World</mark>');
  });

  it('escapes HTML in non-matching surrounding text', () => {
    const result = ctx.highlightMatch('<b>match</b>', 'match');
    expect(result).toBe('&lt;b&gt;<mark class="search-highlight">match</mark>&lt;/b&gt;');
  });

  it('returns escaped text when no match is found', () => {
    expect(ctx.highlightMatch('Hello World', 'xyz')).toBe('Hello World');
  });

  it('returns empty string when text is empty', () => {
    expect(ctx.highlightMatch('', 'query')).toBe('');
  });

  it('returns empty string when text is null', () => {
    expect(ctx.highlightMatch(null, 'query')).toBe('');
  });
});
