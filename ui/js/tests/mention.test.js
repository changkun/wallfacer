/**
 * Tests for the @-mention module: query parsing, filtering and autocomplete flow.
 */
import { describe, it, expect, beforeAll, vi } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

function makeClassList() {
  const set = new Set();
  return {
    add: (cls) => { set.add(cls); },
    remove: (cls) => { set.delete(cls); },
    contains: (cls) => set.has(cls),
  };
}

function createElement(overrides = {}) {
  const classSet = new Set();
  const node = {
    classList: {
      add: (cls) => { classSet.add(cls); },
      remove: (cls) => { classSet.delete(cls); },
      contains: (cls) => classSet.has(cls),
    },
    style: {},
    _children: [],
    _listeners: {},
    dataset: {},
    _parent: null,
    textContent: '',
    innerHTML: '',
    value: '',
    selectionStart: 0,
    selectionEnd: 0,
    addEventListener(type, handler) {
      this._listeners[type] = this._listeners[type] || [];
      this._listeners[type].push(handler);
    },
    dispatchEvent(evt = {}) {
      const handlers = this._listeners[evt.type] || [];
      for (const handler of handlers) {
        handler(evt);
      }
    },
    appendChild(child) {
      child._parent = this;
      this._children.push(child);
      return child;
    },
    remove() {
      if (!this._parent) return;
      this._parent._children = this._parent._children.filter((child) => child !== this);
    },
    querySelector() { return null; },
    querySelectorAll() { return []; },
    focus() { this.focused = true; },
    getBoundingClientRect() { return { bottom: 5, left: 2, width: 100 }; },
    setSelectionRange(start, end) {
      this.selectionStart = start;
      this.selectionEnd = end;
    },
  };
  Object.defineProperty(node, 'className', {
    get: () => Array.from(classSet).join(' '),
    set: (value = '') => {
      classSet.clear();
      String(value).split(/\s+/).filter(Boolean).forEach((cls) => classSet.add(cls));
    },
  });
  return Object.assign(node, overrides);
}

function makeContext(extra = {}) {
  const elements = new Map(extra.elements || []);
  const ctx = {
    console,
    Math,
    Date,
    setTimeout,
    clearTimeout,
    setInterval: () => 0,
    Event: function Event(type, init) {
      this.type = type;
      if (init && init.bubbles) this.bubbles = init.bubbles;
    },
    document: {
      createElement: () => createElement(),
      body: createElement(),
      getElementById: (id) => elements.get(id) || null,
      querySelector: () => null,
      querySelectorAll: () => ({ forEach: () => {} }),
      documentElement: { setAttribute: () => {} },
      readyState: 'complete',
      addEventListener: () => {},
    },
    window: {
      addEventListener: () => {},
    },
    ...extra,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx, filename) {
  const code = readFileSync(join(jsDir, filename), 'utf8');
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

describe('_mentionGetQuery', () => {
  it('returns query when @ is at beginning', () => {
    const ctx = makeContext();
    loadScript(ctx, 'mention.js');

    const textarea = { value: '@setup', selectionStart: 6 };
    const result = ctx._mentionGetQuery(textarea);
    expect(result).toEqual({ query: 'setup', atIdx: 0 });
  });

  it('ignores @-mentions embedded in words', () => {
    const ctx = makeContext();
    loadScript(ctx, 'mention.js');

    const textarea = { value: 'hello@setup', selectionStart: 9 };
    expect(ctx._mentionGetQuery(textarea)).toBe(null);
  });

  it('returns null when query contains whitespace', () => {
    const ctx = makeContext();
    loadScript(ctx, 'mention.js');

    const textarea = { value: '@setup test', selectionStart: 7 };
    expect(ctx._mentionGetQuery(textarea)).toBe(null);
  });
});

describe('_mentionFilter', () => {
  it('returns a file-order-limited top match set', () => {
    const ctx = makeContext();
    loadScript(ctx, 'mention.js');
    const files = Array.from({ length: 25 }, (_, idx) => `files/${String(idx).padStart(2, '0')}-notes.txt`);
    const result = ctx._mentionFilter(files, '10');
    expect(result).toHaveLength(1);
    expect(result[0]).toBe('files/10-notes.txt');
  });

  it('prefers basename matches over path-only matches', () => {
    const ctx = makeContext();
    loadScript(ctx, 'mention.js');
    const files = ['src/main/app.go', 'src/test/notes.go', 'src/main/notes_test.txt'];
    const result = ctx._mentionFilter(files, 'notes');
    expect(result).toEqual(['src/test/notes.go', 'src/main/notes_test.txt']);
  });
});

describe('_mentionLoadFiles', () => {
  it('caches successful responses and returns [] while loading', async () => {
    let resolve;
    const loadingPromise = new Promise((resolveFn) => { resolve = resolveFn; });
    const api = vi.fn().mockImplementation(() => loadingPromise.then(() => ({ files: ['a.go', 'b.go'] })));
    const ctx = makeContext({ api });
    loadScript(ctx, 'mention.js');

    const firstPromise = ctx._mentionLoadFiles();
    const second = await ctx._mentionLoadFiles();
    expect(second).toEqual([]);

    resolve();
    const first = await firstPromise;
    expect(first).toEqual(['a.go', 'b.go']);
    expect(api).toHaveBeenCalledTimes(1);
  });
});

describe('attachMentionAutocomplete', () => {
  beforeAll(() => {
    vi.useFakeTimers();
  });

  it('auto-selects first match so Enter/Tab completes without arrow navigation', async () => {
    const textarea = createElement({
      selectionStart: 5,
      value: '@main',
    });
    const nodes = [
      ['new-prompt', textarea],
      ['modal-edit-prompt', null],
      ['modal-retry-prompt', null],
    ];
    const api = vi.fn().mockResolvedValue({
      // 'lib/main_test.go' scores higher (basename match), 'src/main/app.go' is path-only.
      files: ['src/main/app.go', 'README.md', 'lib/main_test.go'],
    });
    const ctx = makeContext({ elements: nodes, api });
    loadScript(ctx, 'mention.js');
    ctx.attachMentionAutocomplete(textarea);

    const inputHandler = textarea._listeners.input[0];
    const keyHandler = textarea._listeners.keydown;
    const blurHandler = textarea._listeners.blur[0];

    await inputHandler();
    await Promise.resolve();
    const dropdown = ctx.document.body._children.find((n) => n.classList.contains('mention-dropdown'));
    expect(dropdown).toBeTruthy();

    // First item should be auto-selected (has mention-item-selected class).
    const selectedItems = dropdown._children.filter(
      (item) => item.className && item.className.includes('mention-item-selected'),
    );
    expect(selectedItems).toHaveLength(1);

    // Enter without any ArrowDown should complete with the top-ranked file.
    keyHandler[0]({ key: 'Enter', preventDefault: () => {} });
    await Promise.resolve();
    expect(textarea.value).toBe('@lib/main_test.go');

    blurHandler({ type: 'blur' });
    vi.advanceTimersByTime(150);
    expect(ctx.document.body._children.includes(dropdown)).toBe(false);
  });

  it('ArrowDown navigates past the auto-selected first item', async () => {
    const textarea = createElement({
      selectionStart: 5,
      value: '@main',
    });
    const nodes = [
      ['new-prompt', textarea],
      ['modal-edit-prompt', null],
      ['modal-retry-prompt', null],
    ];
    const api = vi.fn().mockResolvedValue({
      files: ['src/main/app.go', 'README.md', 'lib/main_test.go'],
    });
    const ctx = makeContext({ elements: nodes, api });
    loadScript(ctx, 'mention.js');
    ctx.attachMentionAutocomplete(textarea);

    const inputHandler = textarea._listeners.input[0];
    const keyHandler = textarea._listeners.keydown;

    await inputHandler();
    await Promise.resolve();

    // ArrowDown moves from auto-selected 0 to 1.
    keyHandler[0]({ key: 'ArrowDown', preventDefault: () => {} });
    keyHandler[0]({ key: 'Tab', preventDefault: () => {} });
    await Promise.resolve();
    // Second-ranked match is the path-only match.
    expect(textarea.value).toBe('@src/main/app.go');
  });
});
