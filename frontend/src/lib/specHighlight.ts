// Inline highlight for spec comments. The DOM-touching companion to the pure
// logic in specComments.ts: it marks each commented line in the rendered spec
// body the way ai-as-an-infrastructure marks commented prose, so a comment is
// visible *on the text it annotates*, not only as a gutter badge.
//
// Granularity is the source LINE, because that is what the server anchors to.
// The coordinator resolves each thread to a 1-based `line` and stores the *raw*
// source line(s) as `anchor.exact_text` (markdown markup and all: `**bold**`,
// `[link](url)`, ...). That raw text does not substring-match the *rendered* DOM
// text, so we do not try to locate a sub-span; we highlight the whole block at
// the thread's line. Block-scoping reuses the server's line resolution and is
// reliable regardless of inline markup.

import { blockForLine, type SpecCommentThread } from './specComments';

// A rendered source-line block: its 1-based line, vertical offset in the
// scrolling body, and the element itself (for in-block highlighting).
export interface SourceBlock {
  line: number;
  top: number;
  el: HTMLElement;
}

// collectSourceBlocks indexes every data-source-line element under root, with
// its scroll-content offset (for gutter markers) and element (for highlights).
// When an ancestor and its descendant carry the *same* line (e.g. an <ol> and
// its first <li>, both stamped at the list's start line), only the deepest is
// kept: the descendant owns the text, and highlighting the ancestor would skip
// all of it as "nested". One block per line, the most specific one.
export function collectSourceBlocks(root: HTMLElement): SourceBlock[] {
  const rootRect = root.getBoundingClientRect();
  const byLine = new Map<number, HTMLElement>();
  root.querySelectorAll<HTMLElement>('[data-source-line]').forEach((el) => {
    const line = Number.parseInt(el.getAttribute('data-source-line') || '', 10);
    if (!Number.isFinite(line)) return;
    const prev = byLine.get(line);
    // Prefer the deeper element: keep `el` if it is contained by the previous
    // pick (it is more specific), otherwise keep what we have.
    if (!prev || prev.contains(el)) byLine.set(line, el);
  });
  const out: SourceBlock[] = [];
  byLine.forEach((el, line) => {
    const top = el.getBoundingClientRect().top - rootRect.top + root.scrollTop;
    out.push({ line, top, el });
  });
  return out;
}

interface TextNodeRef { node: Text; start: number }

// blockTextNodes collects the highlightable text nodes directly inside a block:
// it skips text inside <pre>/<svg> (code blocks and mermaid, which must not be
// wrapped), inside <a> (so links stay navigable, not swallowed by the mark),
// inside a nested block-level element (a nested list/blockquote is its own
// line), and inside an existing sc-mark (so a re-run does not nest marks).
const NESTED_BLOCKS = 'ul, ol, li, pre, table, blockquote, div';
function blockTextNodes(block: Element): TextNodeRef[] {
  const nodes: TextNodeRef[] = [];
  let offset = 0;
  const walker = document.createTreeWalker(block, NodeFilter.SHOW_TEXT);
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    const t = n as Text;
    const parent = t.parentElement;
    if (parent?.closest('pre, svg, a, mark.sc-mark')) continue;
    // Skip text owned by a nested block (the nested element is its own line and
    // carries its own data-source-line); keep text whose nearest block is this.
    const nested = parent?.closest(NESTED_BLOCKS);
    if (nested && nested !== block && block.contains(nested)) continue;
    nodes.push({ node: t, start: offset });
    offset += t.data.length;
  }
  return nodes;
}

// wrapNodes wraps each given text node in a mark built by makeMark. Returns true
// if anything was wrapped. Wrapping whole nodes (not a sub-range) keeps it
// robust: adjacent marks read as one continuous highlight across inline spans.
function wrapNodes(nodes: TextNodeRef[], makeMark: () => HTMLElement): boolean {
  let wrapped = false;
  for (const { node } of nodes) {
    if (!node.data.trim()) continue; // don't wrap pure whitespace between spans
    const range = document.createRange();
    range.selectNode(node);
    try {
      range.surroundContents(makeMark());
      wrapped = true;
    } catch {
      // Node not safely wrappable in isolation; skip it.
    }
  }
  return wrapped;
}

// clearHighlights unwraps every sc-mark under root, restoring the original text
// nodes. Called before each placement pass so highlights rebuild from scratch
// (a removed or resolved thread leaves no stale mark).
export function clearHighlights(root: HTMLElement): void {
  root.querySelectorAll<HTMLElement>('mark.sc-mark').forEach((mark) => {
    const parent = mark.parentNode;
    if (!parent) return;
    while (mark.firstChild) parent.insertBefore(mark.firstChild, mark);
    parent.removeChild(mark);
    parent.normalize();
  });
}

export interface HighlightOptions {
  openId: string | null;
  onOpen: (threadId: string) => void;
}

// highlightThreads marks the commented line for each inline thread and returns
// the set of thread ids represented by a mark — one "primary" thread per block
// (the first, in input order). The caller renders gutter markers for the rest
// (extra threads on a block, and threads whose block has no wrappable text), so
// every thread keeps a click target and two comments on one line never hide
// each other. Marks at distinct lines never overlap.
export function highlightThreads(
  root: HTMLElement,
  blocks: SourceBlock[],
  threads: SpecCommentThread[],
  opts: HighlightOptions,
): Set<string> {
  clearHighlights(root);
  // Group threads by the block element their line resolves to, preserving order.
  const byEl = new Map<HTMLElement, SpecCommentThread[]>();
  for (const t of threads) {
    const block = blockForLine(blocks, t.line);
    const el = block ? (block as SourceBlock).el : null;
    if (!el) continue;
    const list = byEl.get(el);
    if (list) list.push(t);
    else byEl.set(el, [t]);
  }

  const primary = new Set<string>();
  for (const [el, ts] of byEl) {
    const lead = ts[0];
    const allResolved = ts.every((t) => t.resolved || t.status === 'resolved');
    const open = ts.some((t) => t.id === opts.openId);
    const nodes = blockTextNodes(el);
    const ok = wrapNodes(nodes, () => {
      const mark = document.createElement('mark');
      mark.className = 'sc-mark'
        + (allResolved ? ' sc-mark--resolved' : '')
        + (open ? ' sc-mark--open' : '');
      mark.dataset.threadId = lead.id;
      mark.title = ts.length > 1 ? `${ts.length} comment threads` : 'Comment thread';
      mark.addEventListener('click', (e) => {
        e.stopPropagation();
        opts.onOpen(lead.id);
      });
      return mark;
    });
    if (ok) primary.add(lead.id);
  }
  return primary;
}

// destack pushes markers that snapped to the same (or near-same) vertical
// position apart by `step` px, so fallback gutter markers anchored to one block
// cascade down the gutter instead of stacking on an identical `top` and hiding
// each other. Returns a new, top-sorted list; inputs are not mutated.
export function destack<T extends { top: number }>(markers: T[], step = 20): T[] {
  const sorted = [...markers].sort((a, b) => a.top - b.top);
  const out: T[] = [];
  let prev = -Infinity;
  for (const m of sorted) {
    const top = m.top < prev + step ? prev + step : m.top;
    out.push({ ...m, top });
    prev = top;
  }
  return out;
}

// syncOpenMark reflects the open thread onto already-rendered marks without a
// full rewrap (cheap; runs on every open/close).
export function syncOpenMark(root: HTMLElement, openId: string | null): void {
  root.querySelectorAll<HTMLElement>('mark.sc-mark').forEach((m) => {
    m.classList.toggle('sc-mark--open', m.dataset.threadId === openId);
  });
}
