import { describe, it, expect, beforeEach } from 'vitest';
import {
  clearHighlights,
  collectSourceBlocks,
  destack,
  highlightThreads,
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

  it("highlights the commented line even when it contains markdown markup (the bug: exact_text is raw source, the DOM is rendered)", () => {
    // The rendered DOM has no `**`; the server's exact_text would. Highlighting
    // the block, not matching exact_text, is what makes this work.
    const root = body('<p data-source-line="5">There are <strong>two</strong> wires and they do not merge</p>');
    const primary = highlightThreads(root, collectSourceBlocks(root), [thread({ id: 't1', line: 5 })], { openId: null, onOpen: () => {} });

    expect(primary).toEqual(new Set(['t1']));
    const marks = root.querySelectorAll('mark.sc-mark');
    expect(marks.length).toBeGreaterThan(0);
    // Every visible word of the line is inside a mark, and the rendered text is intact.
    expect(root.querySelector('p')!.textContent).toBe('There are two wires and they do not merge');
    expect([...marks].every((m) => (m as HTMLElement).dataset.threadId === 't1')).toBe(true);
    expect(root.querySelector('strong mark.sc-mark')!.textContent).toBe('two');
  });

  it('groups two comments on one line into a single highlight; only the lead is primary', () => {
    // The other thread is not primary, so the caller renders a gutter marker for
    // it — neither hides the other.
    const root = body('<p data-source-line="5">one commented line</p>');
    const opened: string[] = [];
    const primary = highlightThreads(root, collectSourceBlocks(root), [
      thread({ id: 't1', line: 5 }),
      thread({ id: 't2', line: 5 }),
    ], { openId: null, onOpen: (id) => opened.push(id) });

    expect(primary).toEqual(new Set(['t1']));
    expect(root.querySelectorAll('mark.sc-mark mark.sc-mark')).toHaveLength(0); // no nesting
    (root.querySelector('mark.sc-mark') as HTMLElement).click();
    expect(opened).toEqual(['t1']);
  });

  it('does not wrap links (they stay navigable) but wraps the rest of the line', () => {
    const root = body('<p data-source-line="5">see <a href="x.md">the spec</a> for detail</p>');
    highlightThreads(root, collectSourceBlocks(root), [thread({ id: 't1', line: 5 })], { openId: null, onOpen: () => {} });
    expect(root.querySelector('a mark.sc-mark')).toBeNull();
    expect(root.querySelector('a')!.textContent).toBe('the spec');
    expect(root.querySelectorAll('mark.sc-mark').length).toBeGreaterThan(0);
  });

  it('does not highlight a code/mermaid block (no wrappable text), leaving it to a gutter marker', () => {
    const root = body('<pre data-source-line="5"><code>alpha</code></pre>');
    const primary = highlightThreads(root, collectSourceBlocks(root), [thread({ id: 't1', line: 5 })], { openId: null, onOpen: () => {} });
    expect(primary.has('t1')).toBe(false);
    expect(root.querySelectorAll('mark.sc-mark')).toHaveLength(0);
  });

  it('is idempotent: a second pass does not nest or duplicate marks', () => {
    const root = body('<p data-source-line="5">one commented line</p>');
    const opts = { openId: null, onOpen: () => {} };
    highlightThreads(root, collectSourceBlocks(root), [thread({ id: 't1', line: 5 })], opts);
    const first = root.querySelectorAll('mark.sc-mark').length;
    highlightThreads(root, collectSourceBlocks(root), [thread({ id: 't1', line: 5 })], opts);
    expect(root.querySelectorAll('mark.sc-mark')).toHaveLength(first);
    expect(root.querySelectorAll('mark.sc-mark mark.sc-mark')).toHaveLength(0);
  });

  it('clearHighlights restores the original text', () => {
    const root = body('<p data-source-line="5">one commented line</p>');
    highlightThreads(root, collectSourceBlocks(root), [thread({ id: 't1', line: 5 })], { openId: null, onOpen: () => {} });
    expect(root.querySelectorAll('mark.sc-mark').length).toBeGreaterThan(0);
    clearHighlights(root);
    expect(root.querySelectorAll('mark.sc-mark')).toHaveLength(0);
    expect(root.querySelector('p')!.textContent).toBe('one commented line');
  });

  it('syncOpenMark toggles the open class to the active thread only', () => {
    const root = body('<p data-source-line="5">first line</p><p data-source-line="6">second line</p>');
    highlightThreads(root, collectSourceBlocks(root), [
      thread({ id: 't1', line: 5 }),
      thread({ id: 't2', line: 6 }),
    ], { openId: null, onOpen: () => {} });
    syncOpenMark(root, 't2');
    const open = root.querySelectorAll('mark.sc-mark--open');
    expect(open.length).toBeGreaterThan(0);
    expect([...open].every((m) => (m as HTMLElement).dataset.threadId === 't2')).toBe(true);
  });
});
