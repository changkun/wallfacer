import { describe, it, expect, beforeEach } from 'vitest';
import {
  clearHighlights,
  collectSourceBlocks,
  destack,
  highlightThreads,
  locateQuote,
  syncOpenMark,
} from './specHighlight';
import type { SpecCommentThread } from './specComments';

function thread(p: Partial<SpecCommentThread> & { id: string }): SpecCommentThread {
  return {
    org_id: 'o',
    workspace_id: 'repo',
    spec_path: 'a/b.md',
    anchor: { section_path: [], line_hash: '', prefix: '', suffix: '', exact_text: '', line_hint: 0 },
    author_sub: 'u',
    created_at: '2026-01-01T00:00:00Z',
    resolved: false,
    status: 'active',
    comments: [],
    line: 5,
    orphaned: false,
    ...p,
  };
}

function body(html: string): HTMLElement {
  const el = document.createElement('div');
  el.innerHTML = html;
  document.body.appendChild(el);
  return el;
}

describe('locateQuote', () => {
  it('returns the span of a single occurrence', () => {
    expect(locateQuote('the quick brown fox', 'quick')).toEqual([4, 9]);
  });

  it('returns null when the quote is absent', () => {
    expect(locateQuote('the quick brown fox', 'lazy')).toBeNull();
  });

  it('disambiguates duplicates by surrounding context', () => {
    // "set" appears twice; suffix " A" picks the first, prefix should pick none.
    const text = 'set A and set B';
    expect(locateQuote(text, 'set', '', ' A')).toEqual([0, 3]);
    expect(locateQuote(text, 'set', 'and ', '')).toEqual([10, 13]);
  });
});

describe('destack', () => {
  it('cascades markers sharing a top apart by step, leaving distinct ones alone', () => {
    expect(destack([{ top: 100 }, { top: 100 }, { top: 100 }], 20)).toEqual([
      { top: 100 }, { top: 120 }, { top: 140 },
    ]);
    expect(destack([{ top: 100 }, { top: 300 }], 20)).toEqual([{ top: 100 }, { top: 300 }]);
  });

  it('sorts by top and does not mutate the input', () => {
    const input = [{ top: 200 }, { top: 100 }];
    const out = destack(input, 20);
    expect(out).toEqual([{ top: 100 }, { top: 200 }]);
    expect(input).toEqual([{ top: 200 }, { top: 100 }]);
  });
});

describe('highlightThreads', () => {
  beforeEach(() => { document.body.innerHTML = ''; });

  it('marks two comments on the same block as two distinct, non-overlapping marks', () => {
    // Regression: the old gutter-marker approach gave both same-block threads
    // the same top, so the second hid the first. Inline marks sit on their own
    // text spans and both stay visible.
    const root = body('<p data-source-line="5">alpha goes first and beta goes second</p>');
    const blocks = collectSourceBlocks(root);
    const threads = [
      thread({ id: 't1', line: 5, anchor: { ...thread({ id: 'x' }).anchor, exact_text: 'alpha' } }),
      thread({ id: 't2', line: 5, anchor: { ...thread({ id: 'x' }).anchor, exact_text: 'beta' } }),
    ];
    const opened: string[] = [];
    const highlighted = highlightThreads(root, blocks, threads, { openId: null, onOpen: (id) => opened.push(id) });

    expect(highlighted).toEqual(new Set(['t1', 't2']));
    const marks = root.querySelectorAll('mark.sc-mark');
    expect(marks).toHaveLength(2);
    expect(marks[0].textContent).toBe('alpha');
    expect(marks[1].textContent).toBe('beta');
    expect([...marks].map((m) => (m as HTMLElement).dataset.threadId)).toEqual(['t1', 't2']);

    (marks[1] as HTMLElement).click();
    expect(opened).toEqual(['t2']);
  });

  it('is idempotent: a second pass does not nest or duplicate marks', () => {
    const root = body('<p data-source-line="5">alpha goes first and beta goes second</p>');
    const threads = [
      thread({ id: 't1', line: 5, anchor: { ...thread({ id: 'x' }).anchor, exact_text: 'alpha' } }),
      thread({ id: 't2', line: 5, anchor: { ...thread({ id: 'x' }).anchor, exact_text: 'beta' } }),
    ];
    const opts = { openId: null, onOpen: () => {} };
    highlightThreads(root, collectSourceBlocks(root), threads, opts);
    highlightThreads(root, collectSourceBlocks(root), threads, opts);
    expect(root.querySelectorAll('mark.sc-mark')).toHaveLength(2);
    expect(root.querySelectorAll('mark.sc-mark mark.sc-mark')).toHaveLength(0);
  });

  it('excludes a thread whose anchored text is not present (caller falls back to a gutter marker)', () => {
    const root = body('<p data-source-line="5">only alpha here</p>');
    const threads = [
      thread({ id: 't1', line: 5, anchor: { ...thread({ id: 'x' }).anchor, exact_text: 'alpha' } }),
      thread({ id: 't2', line: 5, anchor: { ...thread({ id: 'x' }).anchor, exact_text: 'missing' } }),
    ];
    const highlighted = highlightThreads(root, collectSourceBlocks(root), threads, { openId: null, onOpen: () => {} });
    expect(highlighted.has('t1')).toBe(true);
    expect(highlighted.has('t2')).toBe(false);
  });

  it('does not wrap text inside code blocks or mermaid svg', () => {
    const root = body('<pre data-source-line="5"><code>alpha</code></pre>');
    const threads = [thread({ id: 't1', line: 5, anchor: { ...thread({ id: 'x' }).anchor, exact_text: 'alpha' } })];
    const highlighted = highlightThreads(root, collectSourceBlocks(root), threads, { openId: null, onOpen: () => {} });
    expect(highlighted.has('t1')).toBe(false);
    expect(root.querySelectorAll('mark.sc-mark')).toHaveLength(0);
  });

  it('clearHighlights restores the original text', () => {
    const root = body('<p data-source-line="5">alpha goes first</p>');
    const threads = [thread({ id: 't1', line: 5, anchor: { ...thread({ id: 'x' }).anchor, exact_text: 'alpha' } })];
    highlightThreads(root, collectSourceBlocks(root), threads, { openId: null, onOpen: () => {} });
    expect(root.querySelectorAll('mark.sc-mark')).toHaveLength(1);
    clearHighlights(root);
    expect(root.querySelectorAll('mark.sc-mark')).toHaveLength(0);
    expect(root.querySelector('p')!.textContent).toBe('alpha goes first');
  });

  it('syncOpenMark toggles the open class to the active thread only', () => {
    const root = body('<p data-source-line="5">alpha goes first and beta goes second</p>');
    const threads = [
      thread({ id: 't1', line: 5, anchor: { ...thread({ id: 'x' }).anchor, exact_text: 'alpha' } }),
      thread({ id: 't2', line: 5, anchor: { ...thread({ id: 'x' }).anchor, exact_text: 'beta' } }),
    ];
    highlightThreads(root, collectSourceBlocks(root), threads, { openId: null, onOpen: () => {} });
    syncOpenMark(root, 't2');
    const open = root.querySelectorAll('mark.sc-mark--open');
    expect(open).toHaveLength(1);
    expect((open[0] as HTMLElement).dataset.threadId).toBe('t2');
  });
});
