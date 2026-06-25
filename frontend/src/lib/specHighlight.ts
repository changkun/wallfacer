// Inline highlight for spec comments. The DOM-touching companion to the pure
// logic in specComments.ts: it wraps each inline thread's anchored text in a
// <mark> inside the rendered spec body, the way ai-as-an-infrastructure marks
// commented prose, so a comment is visible *on the text it annotates* rather
// than only as a gutter badge.
//
// Anchoring stays the server's job (it resolves each thread to a 1-based
// `line`); this module only locates the thread's `anchor.exact_text` *within
// the block element at that line* and wraps it. Block-scoping (not a whole-body
// scan) keeps an exact_text that also appears in an unrelated code block or
// mermaid diagram from being highlighted by mistake, and reuses the server's
// line resolution instead of re-deriving anchoring on the client.

import { blockForLine, type SpecCommentThread } from './specComments';

// A rendered source-line block: its 1-based line, vertical offset in the
// scrolling body, and the element itself (for in-block text search).
export interface SourceBlock {
  line: number;
  top: number;
  el: HTMLElement;
}

// collectSourceBlocks indexes every data-source-line element under root, with
// its scroll-content offset (for gutter markers) and element (for highlights).
export function collectSourceBlocks(root: HTMLElement): SourceBlock[] {
  const out: SourceBlock[] = [];
  const rootRect = root.getBoundingClientRect();
  root.querySelectorAll<HTMLElement>('[data-source-line]').forEach((el) => {
    const line = Number.parseInt(el.getAttribute('data-source-line') || '', 10);
    if (!Number.isFinite(line)) return;
    const top = el.getBoundingClientRect().top - rootRect.top + root.scrollTop;
    out.push({ line, top, el });
  });
  return out;
}

// locateQuote returns the [start, end) offset of `exact` within text, or null
// when absent. Identical quotes are disambiguated by which occurrence's
// surrounding context best matches prefix/suffix (W3C TextQuoteSelector style).
// Pure: the unit-test target for the highlight path.
export function locateQuote(
  text: string,
  exact: string,
  prefix = '',
  suffix = '',
): [number, number] | null {
  if (!exact) return null;
  const positions: number[] = [];
  for (let i = text.indexOf(exact); i !== -1; i = text.indexOf(exact, i + 1)) positions.push(i);
  if (positions.length === 0) return null;
  if (positions.length === 1) return [positions[0], positions[0] + exact.length];
  let best = positions[0];
  let bestScore = -1;
  for (const p of positions) {
    const before = text.slice(Math.max(0, p - prefix.length), p);
    const after = text.slice(p + exact.length, p + exact.length + suffix.length);
    let score = 0;
    if (prefix && before.endsWith(prefix)) score += prefix.length;
    if (suffix && after.startsWith(suffix)) score += suffix.length;
    if (score > bestScore) {
      bestScore = score;
      best = p;
    }
  }
  return [best, best + exact.length];
}

interface TextNodeRef { node: Text; start: number }

// textIndex concatenates an element's text and maps offsets back to text nodes.
// It skips text inside <pre>/<svg> (rendered code blocks and mermaid diagrams,
// which must not be wrapped) and inside an existing sc-mark (so a re-run does
// not nest marks).
function textIndex(root: Element): { text: string; nodes: TextNodeRef[] } {
  const nodes: TextNodeRef[] = [];
  let text = '';
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT);
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    const t = n as Text;
    if (t.parentElement?.closest('pre, svg, mark.sc-mark')) continue;
    nodes.push({ node: t, start: text.length });
    text += t.data;
  }
  return { text, nodes };
}

// markRange wraps the [start, end) text-offset span with marks built by
// makeMark, splitting across text nodes as needed. Returns true if anything was
// wrapped (a single un-wrappable node does not fail the whole span).
function markRange(
  nodes: TextNodeRef[],
  start: number,
  end: number,
  makeMark: () => HTMLElement,
): boolean {
  let wrapped = false;
  for (const { node, start: ns } of nodes) {
    const ne = ns + node.data.length;
    if (ne <= start || ns >= end) continue;
    const s = Math.max(start, ns) - ns;
    const e = Math.min(end, ne) - ns;
    const range = document.createRange();
    range.setStart(node, s);
    range.setEnd(node, e);
    try {
      range.surroundContents(makeMark());
      wrapped = true;
    } catch {
      // Range crosses an element boundary within this node; skip it safely.
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

// highlightThreads wraps each inline thread's anchored text in a clickable
// <mark> inside its source-line block, and returns the set of thread ids it
// actually highlighted. A thread whose text cannot be located inline (anchored
// to a stripped heading, or text living in a skipped code/mermaid block) is
// absent from the set, so the caller can fall back to a gutter marker for it.
// Marks at distinct text spans never overlap, so two comments on one paragraph
// stay both visible (the stacking that hid sibling gutter markers is gone).
export function highlightThreads(
  root: HTMLElement,
  blocks: SourceBlock[],
  threads: SpecCommentThread[],
  opts: HighlightOptions,
): Set<string> {
  clearHighlights(root);
  const highlighted = new Set<string>();
  for (const t of threads) {
    const block = blockForLine(blocks, t.line);
    if (!block) continue;
    const el = (block as SourceBlock).el;
    if (!el) continue;
    const exact = (t.anchor.exact_text || '').trim();
    if (exact.length < 3) continue;
    const { text, nodes } = textIndex(el);
    const span = locateQuote(text, exact, t.anchor.prefix, t.anchor.suffix);
    if (!span) continue;
    const resolved = t.resolved || t.status === 'resolved';
    const ok = markRange(nodes, span[0], span[1], () => {
      const mark = document.createElement('mark');
      mark.className = 'sc-mark'
        + (resolved ? ' sc-mark--resolved' : '')
        + (opts.openId === t.id ? ' sc-mark--open' : '');
      mark.dataset.threadId = t.id;
      mark.title = 'Comment thread';
      mark.addEventListener('click', (e) => {
        e.stopPropagation();
        opts.onOpen(t.id);
      });
      return mark;
    });
    if (ok) highlighted.add(t.id);
  }
  return highlighted;
}

// syncOpenMark reflects the open thread onto already-rendered marks without a
// full rewrap (cheap; runs on every open/close).
export function syncOpenMark(root: HTMLElement, openId: string | null): void {
  root.querySelectorAll<HTMLElement>('mark.sc-mark').forEach((m) => {
    m.classList.toggle('sc-mark--open', m.dataset.threadId === openId);
  });
}
