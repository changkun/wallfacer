<script setup lang="ts">
// Inline spec-comments layer. Mounted inside the focused spec body, it overlays
// gutter markers on commented lines, turns a text selection into a new thread,
// shows a threaded popover per marker, and lists orphaned/outdated threads in a
// triage panel. The backend is authoritative: GET returns repositioned threads
// (server-resolved `line` + `orphaned`), POST returns 202 and the result echoes
// back over SSE, so the client refetches GET on any event rather than mutating
// local state. The whole layer degrades silently when coordination is off (GET
// errors or POST returns 503): no markers, no badge, no toast.
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue';
import { api } from '../../api/client';
import { useSse } from '../../composables/useSse';
import { useAuthStore } from '../../stores/auth';
import { renderMarkdown } from '../../lib/markdown';
import {
  activeCount as countActive,
  blockForLine,
  buildReplyTree,
  initials,
  inlineThreads,
  outOfSyncCount,
  threadPreview,
  triageThreads,
  type SpecCommentThread,
} from '../../lib/specComments';
import {
  clearHighlights,
  collectSourceBlocks,
  highlightThreads,
  syncOpenMark,
  type SourceBlock,
} from '../../lib/specHighlight';

const props = defineProps<{
  // The rendered spec body element (source-line stamped). Replaced on spec
  // switch by the crossfade, so it is a prop we watch, never a captured ref.
  bodyEl: HTMLElement | null;
  // Bumped whenever the rendered HTML changes so the placement pass re-runs.
  contentKey: string;
  // The focused spec id (workspace-relative, no leading specs/). Empty when no
  // spec is focused; the POST `spec` field is this value as-is.
  specPath: string;
}>();

const auth = useAuthStore();

// available gates the whole layer. It starts false and flips true only after a
// successful GET. A failed GET (coordinator off) keeps it false: no chrome.
const available = ref(false);
const threads = ref<SpecCommentThread[]>([]);
const showResolved = ref(false);

// ── Fetch + live updates ───────────────────────────────────────────

async function refresh() {
  try {
    const res = await api<{ threads?: SpecCommentThread[] }>('GET', '/api/spec-comments');
    threads.value = res.threads ?? [];
    available.value = true;
  } catch {
    // Coordination unavailable: leave the layer silent.
    available.value = false;
    threads.value = [];
  }
}

useSse({
  url: '/api/spec-comments/stream',
  listeners: {
    // Any authoritative op refetches GET so threads carry fresh line/orphaned.
    'spec-comment'() { void refresh(); },
    heartbeat() { /* keepalive */ },
  },
  onStaleRestart() { void refresh(); },
});

let statusTimer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  void refresh();
  void fetchStatus();
  // Poll the connection state so the indicator reflects sign-in and the
  // connector dialing/reconnecting without a page reload.
  statusTimer = setInterval(() => void fetchStatus(), 5000);
});
onUnmounted(() => clearInterval(statusTimer));

// ── Derived sets ───────────────────────────────────────────────────

const specThreads = computed(() => inlineThreads(threads.value, props.specPath, {
  showResolved: showResolved.value,
}));
const openCount = computed(() => countActive(threads.value, props.specPath));
const triage = computed(() => triageThreads(threads.value));
// outOfSync drives the repo-level banner: comments that no longer match this
// clone's spec text (anchor lost, or the file changed since the comment).
const outOfSync = computed(() => outOfSyncCount(threads.value));

// ── Coordination opt-in (the data-boundary gate) ───────────────────

// optedIn is the persisted server switch; coordToggleAvailable is true only when
// the server can toggle it. When available but off, the layer shows an enable
// prompt instead of the comment chrome.
const optedIn = ref(false);
const coordToggleAvailable = ref(false);
const enabling = ref(false);
// signedIn gates the whole comment surface: when the user is not signed in, no
// comment chrome appears at all. coordState drives the connection indicator.
const signedIn = ref(false);
const coordState = ref<string>('');

async function fetchStatus() {
  try {
    const s = await api<{
      opted_in?: boolean; available?: boolean; signed_in?: boolean; connected?: boolean; state?: string;
    }>('GET', '/api/coordination/status');
    optedIn.value = !!s.opted_in;
    coordToggleAvailable.value = !!s.available;
    signedIn.value = !!s.signed_in;
    coordState.value = s.state || '';
  } catch {
    coordToggleAvailable.value = false;
    signedIn.value = false;
    coordState.value = '';
  }
}

async function enableCoordination() {
  enabling.value = true;
  try {
    await api('POST', '/api/coordination/opt-in', { enabled: true });
    optedIn.value = true;
    void refresh();
    // The connector takes a moment to dial and sync; refetch shortly after.
    setTimeout(() => void refresh(), 1500);
  } catch {
    // leave the prompt up
  } finally {
    enabling.value = false;
  }
}

// ── Inline highlights + fallback gutter markers ────────────────────
// The primary affordance is an inline <mark> on the anchored text (see
// specHighlight). A gutter marker is rendered only for an inline thread whose
// text could not be located inline (anchored to a stripped heading, or text in
// a skipped code/mermaid block), so it still has a visible click target. Inline
// marks at distinct spans never overlap, so two comments on one paragraph both
// stay visible — the stacking that hid sibling gutter markers is gone.

interface Marker { thread: SpecCommentThread; top: number }
const markers = ref<Marker[]>([]);

// Thread-popover state (declared here so the highlight pass and its open-state
// watch can reference them; the popover handlers live in the popover section).
const openThreadId = ref<string | null>(null);
const replyBody = ref('');

// place runs one combined pass: highlight inline, then gutter-mark the leftovers.
function place() {
  const root = props.bodyEl;
  if (!root) { markers.value = []; return; }
  const blocks: SourceBlock[] = collectSourceBlocks(root);
  const highlighted = highlightThreads(root, blocks, specThreads.value, {
    openId: openThreadId.value,
    onOpen: toggleThread,
  });
  const out: Marker[] = [];
  for (const t of specThreads.value) {
    if (highlighted.has(t.id)) continue;
    const block = blockForLine(blocks, t.line);
    if (!block) continue;
    out.push({ thread: t, top: block.top });
  }
  markers.value = out;
}

watch(
  () => [props.bodyEl, props.contentKey, specThreads.value] as const,
  () => { void nextTick(place); },
  { immediate: true },
);

// Reflect the open thread onto inline marks without a full rewrap.
watch(openThreadId, () => {
  if (props.bodyEl) syncOpenMark(props.bodyEl, openThreadId.value);
});

onUnmounted(() => {
  if (props.bodyEl) clearHighlights(props.bodyEl);
});

// ── Selection → new thread ─────────────────────────────────────────

interface Pending { startLine: number; endLine: number; exact: string; x: number; y: number }
const selection = ref<Pending | null>(null);
const composing = ref(false);
const newBody = ref('');

function lineOf(node: Node | null): number {
  let el = node instanceof Element ? node : node?.parentElement ?? null;
  const host = el?.closest<HTMLElement>('[data-source-line]') ?? null;
  if (!host) return 0;
  return Number.parseInt(host.getAttribute('data-source-line') || '0', 10) || 0;
}

function onSelectionChange() {
  if (!available.value || composing.value) return;
  // Ignore churn while typing into the composer.
  const ae = document.activeElement;
  if (ae && (ae.tagName === 'TEXTAREA' || ae.tagName === 'INPUT')) return;
  const root = props.bodyEl;
  const sel = window.getSelection();
  if (!root || !sel || sel.isCollapsed || sel.rangeCount === 0) { selection.value = null; return; }
  const range = sel.getRangeAt(0);
  if (!root.contains(range.commonAncestorContainer)) { selection.value = null; return; }
  const exact = sel.toString().trim();
  if (exact.length < 3) { selection.value = null; return; }
  // startContainer/endContainer (document order), not anchor/focus, so a
  // backwards drag does not invert the range.
  const a = lineOf(range.startContainer);
  const b = lineOf(range.endContainer);
  if (!a && !b) { selection.value = null; return; }
  const startLine = Math.min(a || b, b || a);
  const endLine = Math.max(a, b);
  const rect = range.getBoundingClientRect();
  selection.value = {
    startLine,
    endLine,
    exact,
    x: rect.left + rect.width / 2,
    y: Math.max(rect.top, 8),
  };
}

let selTimer: ReturnType<typeof setTimeout> | undefined;
function onSelectionChangeDebounced() {
  clearTimeout(selTimer);
  selTimer = setTimeout(onSelectionChange, 250);
}

function startCompose() {
  composing.value = true;
  newBody.value = '';
}

async function submitNew() {
  const sel = selection.value;
  const body = newBody.value.trim();
  if (!sel || !body) return;
  await submit({
    op: 'create',
    spec: props.specPath,
    body,
    start_line: sel.startLine,
    end_line: sel.endLine,
  });
  selection.value = null;
  composing.value = false;
  newBody.value = '';
  window.getSelection()?.removeAllRanges();
}

function cancelCompose() {
  selection.value = null;
  composing.value = false;
  newBody.value = '';
}

// ── Thread popover ─────────────────────────────────────────────────

const openThread = computed<SpecCommentThread | null>(() =>
  threads.value.find((t) => t.id === openThreadId.value) ?? null,
);
const openTree = computed(() => openThread.value ? buildReplyTree(openThread.value.comments) : []);

function toggleThread(id: string) {
  openThreadId.value = openThreadId.value === id ? null : id;
  replyBody.value = '';
}

async function submitReply() {
  const t = openThread.value;
  const body = replyBody.value.trim();
  if (!t || !body) return;
  await submit({ op: 'reply', spec: props.specPath, thread_id: t.id, body });
  replyBody.value = '';
}

async function resolveThread(t: SpecCommentThread) {
  const resolved = t.resolved || t.status === 'resolved';
  await submit({ op: resolved ? 'reopen' : 'resolve', spec: props.specPath, thread_id: t.id });
}

// ── Submit helper ──────────────────────────────────────────────────
// POST returns 202; the coordinator echoes the result over SSE which triggers a
// GET refetch. A 503 (coordination off) is swallowed quietly.

async function submit(payload: Record<string, unknown>): Promise<void> {
  try {
    await api('POST', '/api/spec-comments', payload);
  } catch {
    // Unavailable or rejected: stay silent, the SSE refetch is the source of
    // truth and nothing local was mutated.
  }
}

// ── Author display ─────────────────────────────────────────────────

function authorName(sub: string): string {
  if (auth.me?.principal_id === sub) return auth.me?.display_name || auth.me?.name || auth.me?.email || 'you';
  return sub;
}
function isMe(sub: string): boolean {
  return !!auth.me?.principal_id && auth.me.principal_id === sub;
}
function authorInitials(sub: string): string {
  return initials(isMe(sub) ? authorName(sub) : sub);
}

function renderBody(src: string): string {
  return renderMarkdown(src);
}

// ── Triage panel ───────────────────────────────────────────────────

const triageOpen = ref(false);

async function resolveTriage(t: SpecCommentThread) {
  await submit({ op: 'resolve', spec: t.spec_path, thread_id: t.id });
}

// ── Lifecycle ──────────────────────────────────────────────────────

onMounted(() => {
  document.addEventListener('selectionchange', onSelectionChangeDebounced);
  document.addEventListener('mouseup', onSelectionChange);
});
onUnmounted(() => {
  clearTimeout(selTimer);
  document.removeEventListener('selectionchange', onSelectionChangeDebounced);
  document.removeEventListener('mouseup', onSelectionChange);
});

defineExpose({ openCount, showResolved, available });
</script>

<template>
  <!-- Gated on sign-in: when the user is not signed in, no comment surface
       appears at all (you cannot comment, so it should not show). -->
  <template v-if="available && signedIn">
    <!-- Opt-in prompt: coordination is off by default (the data boundary). -->
    <div v-if="coordToggleAvailable && !optedIn" class="sc-banner sc-banner--optin">
      <span>Spec comments are off. Enable to comment and see your team's comments.</span>
      <button
        type="button"
        class="sc-btn sc-btn--primary"
        :disabled="enabling"
        @click="enableCoordination"
      >{{ enabling ? 'Enabling' : 'Enable' }}</button>
    </div>

    <template v-if="optedIn">
    <!-- Out-of-sync warning: comments that no longer match this clone's specs. -->
    <div v-if="outOfSync > 0" class="sc-banner sc-banner--warn">
      <span>
        {{ outOfSync }} {{ outOfSync === 1 ? 'comment does' : 'comments do' }} not match your
        current spec text. Your copy may be out of sync with your team.
      </span>
      <button
        v-if="triage.length > 0"
        type="button"
        class="sc-banner-link"
        @click="triageOpen = true"
      >Review</button>
    </div>

    <!-- Header strip: open-thread count + Show resolved toggle + triage entry. -->
    <div class="sc-bar">
      <span class="sc-count" :class="{ 'sc-count--zero': openCount === 0 }">
        {{ openCount }} {{ openCount === 1 ? 'comment' : 'comments' }}
      </span>
      <label class="sc-toggle">
        <input type="checkbox" v-model="showResolved" />
        Show resolved
      </label>
      <button
        v-if="triage.length > 0"
        type="button"
        class="sc-triage-btn"
        @click="triageOpen = !triageOpen"
      >Triage {{ triage.length }}</button>
      <!-- Connection indicator so a stalled connection is never a silent 503. -->
      <span
        class="sc-conn"
        :class="'sc-conn--' + coordState"
        :title="'Coordination: ' + coordState"
      >{{ coordState === 'connected' ? 'synced' : coordState === 'connecting' ? 'connecting…' : coordState }}</span>
    </div>

    <!-- Inline gutter markers, positioned over the scrolling body content. -->
    <div class="sc-markers">
      <button
        v-for="m in markers"
        :key="m.thread.id"
        type="button"
        class="sc-marker"
        :class="{
          'sc-marker--resolved': m.thread.resolved || m.thread.status === 'resolved',
          'sc-marker--open': openThreadId === m.thread.id,
        }"
        :style="{ top: m.top + 'px' }"
        :title="threadPreview(m.thread)"
        @click="toggleThread(m.thread.id)"
      >
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/></svg>
        <span class="sc-marker-n">{{ m.thread.comments.length }}</span>
      </button>
    </div>

    <!-- Selection action: a small floating Comment button. -->
    <div
      v-if="selection && !composing"
      class="sc-float"
      :style="{ left: selection.x + 'px', top: (selection.y - 40) + 'px' }"
    >
      <button type="button" class="sc-float-btn" @click="startCompose">Comment</button>
    </div>

    <!-- New-thread composer popover. -->
    <div
      v-if="selection && composing"
      class="sc-popover sc-popover--compose"
      :style="{ left: Math.min(selection.x, 9999) + 'px', top: (selection.y + 12) + 'px' }"
    >
      <blockquote class="sc-quote">{{ selection.exact }}</blockquote>
      <textarea
        v-model="newBody"
        class="sc-textarea"
        rows="3"
        placeholder="Add a comment. Markdown supported."
        autofocus
        @keydown.meta.enter="submitNew"
        @keydown.ctrl.enter="submitNew"
      />
      <div class="sc-actions">
        <button type="button" class="sc-btn sc-btn--primary" :disabled="!newBody.trim()" @click="submitNew">Comment</button>
        <button type="button" class="sc-btn" @click="cancelCompose">Cancel</button>
      </div>
    </div>

    <!-- Thread popover (comment list + reply + resolve). -->
    <div
      v-if="openThread"
      class="sc-popover sc-popover--thread"
      @click.self="openThreadId = null"
    >
      <div class="sc-popover-inner">
        <div class="sc-thread-head">
          <span class="sc-thread-title">
            {{ openThread.resolved || openThread.status === 'resolved' ? 'Resolved thread' : 'Comment thread' }}
          </span>
          <button
            type="button"
            class="sc-btn sc-btn--ghost"
            @click="resolveThread(openThread)"
          >{{ openThread.resolved || openThread.status === 'resolved' ? 'Reopen' : 'Resolve' }}</button>
          <button type="button" class="sc-close" aria-label="Close" @click="openThreadId = null">✕</button>
        </div>
        <div class="sc-comments">
          <div v-for="node in openTree" :key="node.comment.id" class="sc-comment-group">
            <div class="sc-comment">
              <span class="sc-avatar" :class="{ 'sc-avatar--me': isMe(node.comment.author_sub) }">{{ authorInitials(node.comment.author_sub) }}</span>
              <div class="sc-comment-main">
                <div class="sc-comment-meta">
                  <span class="sc-author" :class="{ 'sc-author--muted': !isMe(node.comment.author_sub) }">{{ authorName(node.comment.author_sub) }}</span>
                </div>
                <div class="sc-comment-body prose-content" v-html="renderBody(node.comment.body)" />
              </div>
            </div>
            <div v-for="r in node.replies" :key="r.id" class="sc-comment sc-comment--reply">
              <span class="sc-avatar" :class="{ 'sc-avatar--me': isMe(r.author_sub) }">{{ authorInitials(r.author_sub) }}</span>
              <div class="sc-comment-main">
                <div class="sc-comment-meta">
                  <span class="sc-author" :class="{ 'sc-author--muted': !isMe(r.author_sub) }">{{ authorName(r.author_sub) }}</span>
                </div>
                <div class="sc-comment-body prose-content" v-html="renderBody(r.body)" />
              </div>
            </div>
          </div>
        </div>
        <div class="sc-reply">
          <textarea
            v-model="replyBody"
            class="sc-textarea"
            rows="2"
            placeholder="Reply. Markdown supported."
            @keydown.meta.enter="submitReply"
            @keydown.ctrl.enter="submitReply"
          />
          <div class="sc-actions">
            <button type="button" class="sc-btn sc-btn--primary" :disabled="!replyBody.trim()" @click="submitReply">Reply</button>
          </div>
        </div>
      </div>
    </div>

    <!-- Triage panel: orphaned/outdated threads for the repo. -->
    <div v-if="triageOpen" class="sc-popover sc-popover--triage" @click.self="triageOpen = false">
      <div class="sc-popover-inner">
        <div class="sc-thread-head">
          <span class="sc-thread-title">Triage</span>
          <button type="button" class="sc-close" aria-label="Close" @click="triageOpen = false">✕</button>
        </div>
        <p class="sc-triage-hint">Threads whose anchored text moved or was filed away. They no longer render inline.</p>
        <div class="sc-triage-list">
          <div v-for="t in triage" :key="t.id" class="sc-triage-item">
            <div class="sc-triage-meta">
              <span class="sc-triage-status" :class="'sc-triage-status--' + t.status">{{ t.status }}</span>
              <span class="sc-triage-spec">{{ t.spec_path }}</span>
            </div>
            <blockquote v-if="t.anchor.exact_text" class="sc-quote">{{ t.anchor.exact_text }}</blockquote>
            <div v-if="t.anchor.section_path.length" class="sc-triage-section">{{ t.anchor.section_path.join(' › ') }}</div>
            <div class="sc-triage-preview">{{ threadPreview(t) }}</div>
            <div class="sc-triage-foot">
              <span class="sc-author sc-author--muted">{{ authorName(t.author_sub) }}</span>
              <span class="sc-spacer" />
              <button
                v-if="t.status !== 'outdated'"
                type="button"
                class="sc-btn sc-btn--sm"
                @click="resolveTriage(t)"
              >Resolve</button>
              <button type="button" class="sc-btn sc-btn--sm" disabled title="coming soon">Mark outdated</button>
              <button type="button" class="sc-btn sc-btn--sm" disabled title="coming soon">Re-place</button>
            </div>
          </div>
        </div>
      </div>
    </div>
    </template>
  </template>
</template>

<style scoped>
/* Banners: the opt-in prompt and the out-of-sync warning, above the prose. */
.sc-banner {
  order: -2;
  display: flex;
  align-items: center;
  gap: 0.75rem;
  margin: 0 0 0.5rem;
  padding: 0.5rem 0.75rem;
  border-radius: 6px;
  font-size: 0.85rem;
  line-height: 1.4;
}
.sc-banner--optin {
  background: var(--color-surface-2, #f3f4f6);
  border: 1px solid var(--color-border, #d1d5db);
}
.sc-banner--warn {
  background: color-mix(in srgb, #f59e0b 12%, transparent);
  border: 1px solid color-mix(in srgb, #f59e0b 45%, transparent);
}
.sc-banner span { flex: 1; }
.sc-banner-link {
  background: none;
  border: none;
  padding: 0;
  color: var(--color-accent, #2563eb);
  cursor: pointer;
  font: inherit;
  text-decoration: underline;
}
/* Connection indicator: muted by default, amber while connecting, green synced. */
.sc-conn {
  margin-left: auto;
  font-size: 0.72rem;
  color: var(--color-text-muted, #9ca3af);
}
.sc-conn--connected { color: #16a34a; }
.sc-conn--connecting { color: #d97706; }
.sc-conn--opted-out,
.sc-conn--signed-out { color: var(--color-text-muted, #9ca3af); }

/* Header strip sits at the top of the body, before the prose. */
.sc-bar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 0 0 14px;
  padding-bottom: 8px;
  border-bottom: 1px dashed var(--rule);
  font-size: 12px;
}
.sc-count {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 2px 9px;
  border-radius: 999px;
  background: color-mix(in oklab, var(--accent) 14%, var(--bg-card));
  border: 1px solid color-mix(in oklab, var(--accent) 35%, var(--rule));
  color: var(--accent);
  font-weight: 600;
}
.sc-count--zero {
  background: var(--bg-card);
  border-color: var(--rule);
  color: var(--ink-3);
  font-weight: 500;
}
.sc-toggle {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  color: var(--ink-3);
  cursor: pointer;
}
.sc-toggle input { cursor: pointer; }
.sc-triage-btn {
  margin-left: auto;
  padding: 3px 10px;
  border: 1px solid color-mix(in oklab, var(--warn) 40%, var(--rule));
  border-radius: var(--r-sm);
  background: color-mix(in oklab, var(--warn) 12%, var(--bg-card));
  color: var(--warn);
  font-size: 11px;
  font-weight: 500;
  cursor: pointer;
}
.sc-triage-btn:hover { filter: brightness(0.96); }

/* Markers float in the left gutter of the scrolling body. The body content has
   max-width: 52em so this gutter sits to its left without overlapping prose. */
.sc-markers {
  position: absolute;
  top: 0;
  left: 0;
  width: 0;
  height: 0;
  pointer-events: none;
}
.sc-marker {
  position: absolute;
  left: -6px;
  transform: translateX(-100%);
  display: inline-flex;
  align-items: center;
  gap: 2px;
  padding: 1px 5px 1px 4px;
  border: 1px solid var(--rule);
  border-radius: 999px;
  background: var(--bg-card);
  color: var(--accent);
  cursor: pointer;
  pointer-events: auto;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.12);
}
.sc-marker:hover,
.sc-marker--open {
  border-color: var(--accent);
  background: color-mix(in oklab, var(--accent) 12%, var(--bg-card));
}
.sc-marker--resolved { color: var(--ink-4); opacity: 0.7; }
.sc-marker-n { font-size: 10px; font-weight: 600; }

@media (max-width: 1100px) {
  /* No room for a left gutter on narrow panes: pin markers to the body edge. */
  .sc-marker { left: 2px; transform: none; }
}

/* Floating selection button. */
.sc-float {
  position: fixed;
  transform: translateX(-50%);
  z-index: 50;
}
.sc-float-btn {
  padding: 5px 12px;
  border: none;
  border-radius: var(--r-md);
  background: var(--accent);
  color: #fff;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.25);
}

/* Popovers. */
.sc-popover {
  font-size: 13px;
}
.sc-popover--compose {
  position: fixed;
  transform: translateX(-50%);
  width: 320px;
  max-width: calc(100vw - 24px);
  z-index: 51;
  padding: 12px;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-md);
  box-shadow: 0 8px 30px rgba(0, 0, 0, 0.3);
}
.sc-popover--thread,
.sc-popover--triage {
  position: fixed;
  inset: 0;
  z-index: 52;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 60px 16px 16px;
  background: rgba(0, 0, 0, 0.18);
}
.sc-popover-inner {
  width: 420px;
  max-width: 100%;
  max-height: 80vh;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-lg, 14px);
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.35);
}
.sc-thread-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 12px;
  border-bottom: 1px solid var(--rule);
}
.sc-thread-title { font-weight: 600; color: var(--ink); margin-right: auto; }
.sc-close {
  border: none;
  background: transparent;
  color: var(--ink-3);
  cursor: pointer;
  font-size: 13px;
  padding: 2px 4px;
}
.sc-close:hover { color: var(--ink); }

.sc-comments {
  flex: 1;
  overflow-y: auto;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.sc-comment-group { display: flex; flex-direction: column; gap: 12px; }
.sc-comment { display: flex; gap: 9px; }
.sc-comment--reply { margin-left: 20px; }
.sc-avatar {
  flex: none;
  width: 26px;
  height: 26px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: var(--bg-sunk);
  color: var(--ink-3);
  font-size: 10px;
  font-weight: 600;
}
.sc-avatar--me { background: var(--accent); color: #fff; }
.sc-comment-main { flex: 1; min-width: 0; }
.sc-comment-meta { font-size: 12px; margin-bottom: 2px; }
.sc-author { color: var(--ink); font-weight: 600; }
.sc-author--muted { color: var(--ink-3); font-weight: 500; }
.sc-comment-body { font-size: 13px; line-height: 1.55; }
.sc-comment-body :deep(p) { margin: 0.3em 0; }
.sc-comment-body :deep(p:first-child) { margin-top: 0; }
.sc-comment-body :deep(code) {
  font-family: var(--font-mono);
  font-size: 0.9em;
  background: var(--bg-sunk);
  padding: 1px 4px;
  border-radius: 4px;
}

.sc-reply {
  padding: 10px 12px;
  border-top: 1px solid var(--rule);
}

.sc-quote {
  margin: 0 0 8px;
  padding: 3px 9px;
  border-left: 2px solid var(--accent);
  color: var(--ink-3);
  font-size: 12px;
  max-height: 56px;
  overflow: hidden;
}

.sc-textarea {
  width: 100%;
  box-sizing: border-box;
  resize: vertical;
  padding: 8px 10px;
  border: 1px solid var(--rule);
  border-radius: var(--r-md);
  background: var(--bg-sunk);
  color: var(--ink);
  font-family: inherit;
  font-size: 13px;
  line-height: 1.5;
}
.sc-textarea:focus-visible {
  outline: none;
  border-color: color-mix(in oklab, var(--accent) 50%, var(--rule));
}

.sc-actions { display: flex; gap: 8px; margin-top: 8px; }
.sc-btn {
  padding: 5px 12px;
  border: 1px solid var(--rule);
  border-radius: var(--r-md);
  background: var(--bg-card);
  color: var(--ink-2);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}
.sc-btn:hover:not(:disabled) { background: var(--bg-hover); }
.sc-btn:disabled { opacity: 0.5; cursor: default; }
.sc-btn--primary { background: var(--accent); color: #fff; border-color: var(--accent); }
.sc-btn--primary:hover:not(:disabled) { filter: brightness(0.96); background: var(--accent); }
.sc-btn--ghost { background: transparent; }
.sc-btn--sm { padding: 3px 9px; font-size: 11px; }

/* Triage. */
.sc-triage-hint {
  margin: 0;
  padding: 10px 12px 0;
  font-size: 12px;
  color: var(--ink-3);
}
.sc-triage-list {
  overflow-y: auto;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.sc-triage-item {
  border: 1px solid var(--rule);
  border-radius: var(--r-md);
  padding: 10px;
}
.sc-triage-meta { display: flex; align-items: center; gap: 8px; margin-bottom: 6px; }
.sc-triage-status {
  padding: 1px 7px;
  border-radius: var(--r-sm);
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.sc-triage-status--orphaned {
  color: var(--warn);
  background: color-mix(in oklab, var(--warn) 14%, var(--bg-card));
}
.sc-triage-status--outdated { color: var(--ink-4); background: var(--bg-sunk); }
.sc-triage-spec {
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sc-triage-section { font-size: 11px; color: var(--ink-3); margin: 4px 0; }
.sc-triage-preview { font-size: 12px; color: var(--ink-2); margin: 4px 0; }
.sc-triage-foot { display: flex; align-items: center; gap: 6px; margin-top: 8px; }
.sc-spacer { flex: 1; }
</style>
