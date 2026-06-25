// Pure logic for the inline spec-comments layer. The DOM-touching and Vue parts
// live in the SpecComments components; everything testable in isolation lives
// here: the comment/thread types, source-line lookup against a rendered body,
// thread to marker grouping, the SSE-apply reducer, and author display helpers.
//
// Anchoring is the server's job: the GET response carries `line` (1-based source
// line the anchor currently resolves to) and `orphaned` per thread, and the POST
// body is line-based. The client never builds a text-quote anchor; it only maps
// lines onto `data-source-line` attributes that markdown.ts stamps in the body.

export interface Anchor {
  section_path: string[];
  line_hash: string;
  prefix: string;
  suffix: string;
  exact_text: string;
  line_hint: number;
  commit_sha?: string;
  blob_sha?: string;
}

export interface Comment {
  id: string;
  thread_id: string;
  parent_id?: string;
  author_sub: string;
  body: string;
  created_at: string;
  edited_at?: string;
}

export type ThreadStatus = 'active' | 'resolved' | 'orphaned' | 'outdated';

export interface Thread {
  id: string;
  org_id: string;
  workspace_id: string;
  spec_path: string;
  anchor: Anchor;
  author_sub: string;
  created_at: string;
  resolved: boolean;
  resolved_by?: string;
  resolved_at?: string;
  status: ThreadStatus;
  comments: Comment[];
}

// SpecCommentThread is a Thread plus the server's display-time reposition:
// `line` is the 1-based current source line (0 when orphaned), `orphaned` is
// true when the anchor could not be resolved against the current body.
export interface SpecCommentThread extends Thread {
  line: number;
  orphaned: boolean;
  // outdated is advisory: the spec file changed since the comment was made (the
  // stored blob differs from the file's current blob), even when the anchored
  // line still resolves. Drives the repo out-of-sync banner.
  outdated?: boolean;
}

// outOfSyncCount counts threads whose anchor was lost (orphaned) or whose spec
// text changed since the comment was made (outdated), across all specs in the
// repo. A non-zero count means this clone differs from where teammates
// commented: the signal for the out-of-sync banner.
export function outOfSyncCount(threads: SpecCommentThread[]): number {
  return threads.filter((t) => t.orphaned || t.status === 'orphaned' || t.outdated).length;
}

// The SSE event shape from GET /api/spec-comments/stream (event: spec-comment).
// `op === "sync"` replaces a repo's whole set with `threads`; any other op
// upserts `thread` by id. The streamed Thread lacks `line`/`orphaned` (those are
// computed by GET against the live body), so the component refetches GET on any
// event to get repositioned threads. The reducer below exists for tests and an
// optional incremental path.
export interface SpecCommentEvent {
  op: string;
  repo: string;
  thread?: Thread;
  threads?: Thread[];
}

// rootComment returns the thread's root (parent_id empty), or undefined.
export function rootComment(t: Thread): Comment | undefined {
  return t.comments.find((c) => !c.parent_id);
}

// threadPreview is the short text shown on a marker or in triage: the root
// comment body, single-lined and clamped.
export function threadPreview(t: Thread, max = 80): string {
  const root = rootComment(t);
  const text = (root?.body ?? '').replace(/\s+/g, ' ').trim();
  return text.length > max ? text.slice(0, max - 1) + '…' : text;
}

// buildReplyTree groups a flat comment list into roots with one level of
// replies, ordered by created_at. Replies whose parent is missing attach to the
// nearest root so nothing is dropped.
export interface CommentNode {
  comment: Comment;
  replies: Comment[];
}
export function buildReplyTree(comments: Comment[]): CommentNode[] {
  const sorted = [...comments].sort((a, b) => a.created_at.localeCompare(b.created_at));
  const roots: CommentNode[] = [];
  const byId = new Map<string, CommentNode>();
  for (const c of sorted) {
    if (!c.parent_id) {
      const node = { comment: c, replies: [] as Comment[] };
      roots.push(node);
      byId.set(c.id, node);
    }
  }
  for (const c of sorted) {
    if (!c.parent_id) continue;
    const parent = byId.get(c.parent_id) ?? roots[0];
    if (parent) parent.replies.push(c);
  }
  return roots;
}

// threadsForSpec filters a thread list to one spec path. The POST `spec` field
// is `focusedSpecPath` as-is (no leading "specs/"), and the server round-trips
// it into `spec_path`, so the match is an exact equality.
export function threadsForSpec(
  threads: SpecCommentThread[],
  specPath: string,
): SpecCommentThread[] {
  if (!specPath) return [];
  return threads.filter((t) => t.spec_path === specPath);
}

export interface MarkerOptions {
  showResolved: boolean;
}

// inlineThreads selects the threads that render as inline gutter markers for a
// spec: active (non-orphaned) threads always, resolved threads only when
// `showResolved`. Orphaned and outdated never render inline (they go to triage).
// A resolved line=0 (lost anchor) is excluded since it has nowhere to render.
export function inlineThreads(
  threads: SpecCommentThread[],
  specPath: string,
  opts: MarkerOptions,
): SpecCommentThread[] {
  return threadsForSpec(threads, specPath).filter((t) => {
    if (t.orphaned || t.status === 'orphaned' || t.status === 'outdated') return false;
    if (t.line <= 0) return false;
    if (t.resolved || t.status === 'resolved') return opts.showResolved;
    return true;
  });
}

// activeCount is the header badge: active, non-resolved, non-orphaned threads on
// the spec. Matches the server's "highlighted" rule (Active && !Resolved).
export function activeCount(threads: SpecCommentThread[], specPath: string): number {
  return threadsForSpec(threads, specPath).filter(
    (t) => t.status === 'active' && !t.resolved && !t.orphaned,
  ).length;
}

// triageThreads selects orphaned (and outdated) threads for a repo's triage
// panel, across all specs. These are threads whose anchor was lost or that a
// human filed away; they never render inline.
export function triageThreads(threads: SpecCommentThread[]): SpecCommentThread[] {
  return threads.filter(
    (t) => t.orphaned || t.status === 'orphaned' || t.status === 'outdated',
  );
}

// applyEvent folds one SSE event into a repo-keyed thread map. `sync` replaces
// the repo's whole set; any other op upserts `thread` by id within its repo.
// Returned map is a new object (callers treat it immutably). The streamed
// threads carry no `line`/`orphaned`, so consumers that need repositioned values
// refetch GET; this reducer keeps the optional incremental path honest and is
// the pure unit-test target.
export function applyEvent(
  byRepo: Record<string, Thread[]>,
  ev: SpecCommentEvent,
): Record<string, Thread[]> {
  if (!ev.repo) return byRepo;
  const next = { ...byRepo };
  if (ev.op === 'sync') {
    next[ev.repo] = [...(ev.threads ?? [])];
    return next;
  }
  if (!ev.thread) return next;
  const existing = next[ev.repo] ?? [];
  const idx = existing.findIndex((t) => t.id === ev.thread!.id);
  const updated = idx >= 0
    ? existing.map((t, i) => (i === idx ? ev.thread! : t))
    : [...existing, ev.thread];
  next[ev.repo] = updated;
  return next;
}

// A rendered block's source line paired with its vertical offset in the body.
// Collected from the DOM (elements carrying data-source-line) by the component.
export interface BlockLine {
  line: number;
  top: number;
}

// blockForLine returns the BlockLine whose source line is the largest line <=
// target (the block that contains the target line), or null when none precede
// it. A given line rarely starts a block exactly, so this snaps a thread.line or
// a selection-end line to the enclosing block. `blocks` need not be sorted.
export function blockForLine(blocks: BlockLine[], target: number): BlockLine | null {
  let best: BlockLine | null = null;
  for (const b of blocks) {
    if (b.line <= target && (best === null || b.line > best.line)) best = b;
  }
  // Fall back to the first block when the target precedes everything (e.g. a
  // selection that starts in the stripped title region).
  if (best === null && blocks.length > 0) {
    best = blocks.reduce((a, b) => (b.line < a.line ? b : a));
  }
  return best;
}

// A margin-rail card to lay out: its id, the desired top (its anchor's vertical
// offset in the scrolling body) and its measured height. `top`/`height` are px.
export interface CardBox {
  id: string;
  top: number;
  height: number;
}

// layoutCards places margin cards so none overlap, keeping each as close to its
// anchor as possible. It is the variable-height generalization of destack: sort
// by desired top, then flow top-down, pushing a card to `prevBottom + gap` when
// its anchor would land it inside the previous card. Clustered anchors therefore
// push later cards below their exact line (accepted Overleaf/Confluence
// behavior). Pure and input-order-independent; returns a map of id -> resolved
// top so the caller positions each card without re-sorting its render list.
export function layoutCards(cards: CardBox[], gap = 8): Map<string, number> {
  const sorted = [...cards].sort((a, b) => a.top - b.top || a.id.localeCompare(b.id));
  const out = new Map<string, number>();
  let prevBottom = -Infinity;
  for (const c of sorted) {
    const top = c.top < prevBottom + gap ? prevBottom + gap : c.top;
    out.set(c.id, top);
    prevBottom = top + Math.max(0, c.height);
  }
  return out;
}

// initials derives a 2-char avatar fallback from an author identifier. Used for
// authors other than the signed-in user (there is no members API yet, so a
// sub to initials fallback is the display).
export function initials(idOrName: string): string {
  const s = (idOrName || '?').trim();
  const parts = s.split(/[\s@._-]+/).filter(Boolean);
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
  return s.slice(0, 2).toUpperCase();
}
