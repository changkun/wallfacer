<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue';
import { storeToRefs } from 'pinia';
import { api, authHeaders } from '../../api/client';
import { renderMarkdown } from '../../lib/markdown';
import { enhanceMermaid } from '../../lib/mermaidRender';
import { parseSpecFrontmatter } from '../../lib/specFrontmatter';
import { useRouter } from 'vue-router';
import { usePlanningStore } from '../../stores/planning';
import { useTaskStore } from '../../stores/tasks';
import { useToastStore } from '../../stores/toast';
import { useDialogStore } from '../../stores/dialog';
import FloatingToc from './FloatingToc.vue';

withDefaults(defineProps<{ chatVisible: boolean; chatEnabled?: boolean }>(), {
  chatEnabled: true,
});
const emit = defineEmits<{ toggleChat: []; focusSibling: [path: string]; sendChat: [text: string] }>();

const planning = usePlanningStore();
const tasks = useTaskStore();
const toast = useToastStore();
const dialog = useDialogStore();
const router = useRouter();
const {
  focusedSpecPath, focusedIsIndex, focusedNode, tree,
  focusedTaskId, focusedTaskTitle, focusedTaskPrompt,
} = storeToRefs(planning);

const specText = ref<string>('');
const loading = ref(false);
const loadEpoch = ref(0);
let pollTimer: ReturnType<typeof setInterval> | null = null;

const workspace = computed(() => {
  const ws = tasks.config?.workspaces ?? [];
  return ws.length > 0 ? ws[0] : '';
});

async function loadCurrent() {
  const ws = workspace.value;
  if (!ws || !focusedSpecPath.value) {
    specText.value = '';
    return;
  }
  const myEpoch = ++loadEpoch.value;
  loading.value = true;
  // For Roadmap (specs/README.md) the path itself is already the file
  // path; for regular specs the tree gives a workspace-relative path.
  const absPath = ws + '/' + focusedSpecPath.value;
  const url =
    '/api/explorer/file?path=' + encodeURIComponent(absPath) +
    '&workspace=' + encodeURIComponent(ws);
  try {
    const res = await fetch(url, { headers: authHeaders(), credentials: 'same-origin' });
    if (!res.ok) throw new Error('HTTP ' + res.status);
    const text = await res.text();
    if (myEpoch !== loadEpoch.value) return;
    specText.value = text;
  } catch (e) {
    if (myEpoch !== loadEpoch.value) return;
    console.error('spec load:', e);
    specText.value = '';
  } finally {
    if (myEpoch === loadEpoch.value) loading.value = false;
  }
}

function startPoll() {
  stopPoll();
  pollTimer = setInterval(loadCurrent, 2000);
}

function stopPoll() {
  if (pollTimer) {
    clearInterval(pollTimer);
    pollTimer = null;
  }
}

watch([focusedSpecPath, focusedIsIndex, focusedTaskId, workspace], () => {
  // Task-mode focus owns the body; spec poller must stand down so it
  // doesn't overwrite the task prompt with stale spec content.
  if (focusedTaskId.value) {
    stopPoll();
    specText.value = '';
    return;
  }
  specText.value = '';
  void loadCurrent();
  if (focusedSpecPath.value && !focusedIsIndex.value) startPoll();
  else stopPoll();
}, { immediate: true });

onUnmounted(stopPoll);

// ── Parsed view ────────────────────────────────────────────────────

const parsed = computed(() => parseSpecFrontmatter(specText.value));

const displayTitle = computed(() => {
  if (focusedTaskId.value) return focusedTaskTitle.value || 'Task prompt';
  if (focusedIsIndex.value) return 'Roadmap';
  return parsed.value.frontmatter.title || focusedSpecPath.value || '';
});

const displayPath = computed(() => {
  if (focusedTaskId.value) return 'Task: ' + focusedTaskId.value.slice(0, 8);
  if (focusedIsIndex.value) return '';
  if (!focusedSpecPath.value) return '';
  return focusedSpecPath.value.startsWith('specs/')
    ? focusedSpecPath.value
    : 'specs/' + focusedSpecPath.value;
});

const renderedTaskPrompt = computed(() =>
  focusedTaskPrompt.value ? renderMarkdown(focusedTaskPrompt.value) : '',
);

const status = computed(() => parsed.value.frontmatter.status ?? '');
const effort = computed(() => parsed.value.frontmatter.effort ?? '');
const isArchived = computed(() => status.value === 'archived');

const isLeaf = computed(() => focusedNode.value?.is_leaf ?? true);
const kindLabel = computed(() => (isLeaf.value ? 'implementation' : 'design'));

// Dispatched task — frontmatter exposes the linked task id; show it as
// a click-through pill in the header so users can jump back to the
// running task without leaving Plan mode. Mirrors legacy spec-mode.js
// _highlightTaskId.
const dispatchedTaskId = computed(() => {
  const raw = parsed.value.frontmatter.dispatched_task_id;
  return raw && raw !== 'null' ? raw : '';
});
function openDispatchedTask() {
  if (!dispatchedTaskId.value) return;
  void router.push({ path: '/', query: { task: dispatchedTaskId.value } });
}

const metaParts = computed(() => {
  const out: string[] = [];
  const fm = parsed.value.frontmatter;
  if (fm.author) out.push('Author: ' + fm.author);
  if (fm.created) out.push('Created: ' + fm.created);
  if (fm.updated) out.push('Updated: ' + fm.updated);
  return out.join(' · ');
});

const renderedBody = computed(() => {
  if (!parsed.value.body) return '';
  let html = renderMarkdown(parsed.value.body);
  // Strip the leading <h1> and <hr> so they don't duplicate the title bar.
  html = html.replace(/^\s*<h1\b[^>]*>[\s\S]*?<\/h1>\s*/, '');
  html = html.replace(/^\s*<hr\s*\/?>\s*/, '');
  return html;
});

const showDispatch = computed(
  () => !focusedIsIndex.value && status.value === 'validated' && isLeaf.value && !isArchived.value,
);
const showBreakdown = computed(
  () => !focusedIsIndex.value && (status.value === 'validated' || status.value === 'drafted') && !isArchived.value,
);
const canArchive = computed(
  () =>
    !focusedIsIndex.value &&
    (status.value === 'vague' ||
      status.value === 'drafted' ||
      status.value === 'complete' ||
      status.value === 'stale'),
);

// ── Action buttons ─────────────────────────────────────────────────

interface DispatchResp {
  dispatched?: { spec_path: string; task_id: string }[];
}

const actionBusy = ref(false);

function focusedChildCount(): number {
  if (!focusedNode.value || focusedNode.value.is_leaf) return 0;
  let count = 0;
  const queue = [...(focusedNode.value.children ?? [])];
  while (queue.length > 0) {
    const path = queue.shift()!;
    count++;
    const child = tree.value.find(n => n.path === path);
    if (child?.children) queue.push(...child.children);
  }
  return count;
}

async function onDispatch() {
  if (!focusedSpecPath.value) return;
  if (!(await dialog.confirm({
    title: 'Dispatch spec',
    message: 'Dispatch this spec to the task board?',
    confirmLabel: 'Dispatch',
  }))) return;
  actionBusy.value = true;
  try {
    const resp = await api<DispatchResp>('POST', '/api/specs/transition', {
      action: 'dispatch',
      paths: [focusedSpecPath.value],
      run: false,
    });
    await loadCurrent();
    const taskId = resp.dispatched?.[0]?.task_id;
    if (taskId) {
      toast.pushWithAction('Spec dispatched to the board', 'View on Board →', () => {
        router.push({ path: '/', query: { task: taskId } });
      }, { kind: 'success' });
    } else {
      toast.push('Spec dispatched', { kind: 'success' });
    }
  } catch (e) {
    toast.push('Dispatch failed: ' + (e instanceof Error ? e.message : String(e)), { kind: 'error' });
  } finally {
    actionBusy.value = false;
  }
}

function onBreakdown() {
  emit('sendChat', '/break-down');
}

interface ArchiveAction {
  action: 'archive' | 'unarchive';
  path: string;
}

const toasts = ref<{ id: number; text: string; action: ArchiveAction }[]>([]);
let toastSeq = 0;

function showToast(text: string, action: ArchiveAction) {
  const id = ++toastSeq;
  toasts.value.push({ id, text, action });
  setTimeout(() => dismissToast(id), 8000);
}

function dismissToast(id: number) {
  toasts.value = toasts.value.filter(t => t.id !== id);
}

async function callSpecTransition(action: ArchiveAction['action'], path: string): Promise<boolean> {
  try {
    await api('POST', '/api/specs/transition', { action, path });
    return true;
  } catch (e) {
    await dialog.alert(e instanceof Error ? e.message : String(e));
    return false;
  }
}

async function onArchive() {
  if (!focusedSpecPath.value) return;
  const childCount = focusedChildCount();
  if (childCount > 0 && !(await dialog.confirm({
    title: 'Archive spec',
    message: `Archiving will hide ${childCount} descendant spec(s). Continue?`,
    confirmLabel: 'Archive',
    danger: true,
  }))) {
    return;
  }
  const path = focusedSpecPath.value;
  if (await callSpecTransition('archive', path)) {
    showToast('Spec archived: ' + path, { action: 'archive', path });
    await loadCurrent();
  }
}

async function onUnarchive() {
  if (!focusedSpecPath.value) return;
  const path = focusedSpecPath.value;
  if (await callSpecTransition('unarchive', path)) {
    showToast('Spec unarchived: ' + path, { action: 'unarchive', path });
    await loadCurrent();
  }
}

async function undoToast(toast: { id: number; action: ArchiveAction }) {
  const reverseAction = toast.action.action === 'archive' ? 'unarchive' : 'archive';
  if (await callSpecTransition(reverseAction, toast.action.path)) {
    dismissToast(toast.id);
    await loadCurrent();
  }
}

// ── Spec link interception ────────────────────────────────────────

const bodyRef = ref<HTMLElement | null>(null);

function isSpecLink(href: string): boolean {
  if (!href) return false;
  if (/^([a-z]+:|#|\/\/)/i.test(href)) return false;
  return /\.md(\?|#|$)/.test(href);
}

function resolveSpecHref(href: string): string {
  // Strip any query/anchor portion, keep just the file path.
  const cleaned = href.split('#')[0].split('?')[0];
  if (cleaned.startsWith('specs/')) return cleaned;
  // Relative to focused spec's directory.
  const base = focusedSpecPath.value || '';
  const baseDir = base.includes('/') ? base.slice(0, base.lastIndexOf('/')) : '';
  if (cleaned.startsWith('./')) return joinPath(baseDir, cleaned.slice(2));
  if (cleaned.startsWith('../')) {
    const segs = baseDir.split('/').filter(Boolean);
    let target = cleaned;
    while (target.startsWith('../')) {
      segs.pop();
      target = target.slice(3);
    }
    return [...segs, target].filter(Boolean).join('/');
  }
  return joinPath(baseDir, cleaned);
}

function joinPath(a: string, b: string): string {
  if (!a) return b;
  return a.replace(/\/+$/, '') + '/' + b.replace(/^\/+/, '');
}

function onBodyClick(ev: MouseEvent) {
  const target = ev.target as HTMLElement;
  const a = target.closest('a') as HTMLAnchorElement | null;
  if (!a) return;
  const href = a.getAttribute('href');
  if (!href) return;
  if (!isSpecLink(href)) return;
  ev.preventDefault();
  const resolved = resolveSpecHref(href);
  emit('focusSibling', resolved);
}

// Re-run mermaid enhancement on every body change, including the initial
// mount (immediate) and navigation between specs. bodyRef is in the source
// list because the keyed <main> is replaced through an out-in crossfade: when
// a new spec is focused, renderedBody changes before the new element mounts,
// so the body content alone fires too early (the old element is still fading
// out). bodyRef updates once the new <main> mounts, re-firing this watch
// against the live element. enhanceMermaid is idempotent — already-rendered
// blocks carry the .mermaid-rendered marker and are skipped on later passes.
watch([renderedBody, renderedTaskPrompt, bodyRef], () => {
  void nextTick(() => {
    if (bodyRef.value) void enhanceMermaid(bodyRef.value);
  });
}, { immediate: true });

onMounted(() => {
  bodyRef.value?.addEventListener('click', onBodyClick);
});
onUnmounted(() => {
  bodyRef.value?.removeEventListener('click', onBodyClick);
});

// Exposed for the PlanPage keyboard shortcuts (d = dispatch, b = break down).
function dispatchFocused() { if (showDispatch.value) void onDispatch(); }
function breakdownFocused() { if (showBreakdown.value) onBreakdown(); }
defineExpose({ dispatchFocused, breakdownFocused });
</script>

<template>
  <!-- Crossfade the whole view when the focused entry changes (spec ↔ index ↔
       spec). Vue's out-in transition cancels an in-flight fade when a newer
       focus lands, which is the click-spam epoch-guard from spec-mode.js. -->
  <Transition name="sf-crossfade" mode="out-in">
  <main class="spec-focused" :key="(focusedIsIndex ? 'index' : focusedSpecPath) || 'empty'">
    <header class="sf-header">
      <div class="sf-chrome">
        <span class="sf-path">{{ displayPath }}</span>
        <span v-if="status" class="sf-status" :class="'sf-status--' + status">{{ status }}</span>
        <span
          v-if="!focusedIsIndex && focusedSpecPath"
          class="sf-kind"
          :class="'sf-kind--' + (isLeaf ? 'impl' : 'design')"
        >{{ kindLabel }}</span>
        <span v-if="effort" class="sf-effort">{{ effort }}</span>
        <button
          v-if="dispatchedTaskId"
          type="button"
          class="sf-dispatched-pill"
          :title="`Linked task ${dispatchedTaskId} — click to open on board`"
          @click="openDispatchedTask"
        >→ task {{ dispatchedTaskId.slice(0, 8) }}</button>
        <span class="sf-spacer" />
        <button
          v-if="canArchive"
          type="button"
          class="sf-action"
          :disabled="actionBusy"
          title="Archive this spec (hide from live graph)"
          @click="onArchive"
        >Archive</button>
        <button
          v-if="isArchived"
          type="button"
          class="sf-action"
          :disabled="actionBusy"
          title="Unarchive this spec"
          @click="onUnarchive"
        >Unarchive</button>
        <button
          v-if="showBreakdown"
          type="button"
          class="sf-action"
          :disabled="actionBusy"
          @click="onBreakdown"
        >Break Down</button>
        <button
          v-if="chatEnabled"
          type="button"
          class="sf-action sf-chat-toggle"
          :class="{ 'sf-chat-toggle--folded': !chatVisible }"
          :aria-pressed="chatVisible"
          :title="chatVisible ? 'Hide chat pane (C)' : 'Show chat pane (C)'"
          @click="emit('toggleChat')"
        >Chat</button>
        <button
          v-if="showDispatch"
          type="button"
          class="sf-action sf-dispatch"
          :disabled="actionBusy"
          @click="onDispatch"
        >Dispatch</button>
      </div>
      <span v-if="displayTitle" class="sf-title">{{ displayTitle }}</span>
    </header>

    <div v-if="metaParts" class="sf-meta">{{ metaParts }}</div>

    <div v-if="isArchived" class="sf-archived-banner" role="status">
      <span aria-hidden="true">⊘</span>
      <span>Archived — read-only. Hidden from the live graph and drift checks.</span>
      <button type="button" class="sf-action" @click="onUnarchive">Unarchive</button>
    </div>

    <div
      class="sf-body"
      :key="(focusedTaskId || focusedSpecPath) + ':' + (focusedIsIndex ? '1' : '0')"
    >
      <div v-if="focusedTaskId">
        <div
          v-if="renderedTaskPrompt"
          ref="bodyRef"
          class="sf-content prose-content"
          v-html="renderedTaskPrompt"
        />
        <div v-else class="sf-loading">No prompt for this task.</div>
      </div>
      <template v-else>
        <div v-if="loading && !specText" class="sf-loading">Loading…</div>
        <div
          v-else-if="parsed.warning"
          class="sf-frontmatter-warning"
          role="alert"
        >⚠ {{ parsed.warning }}</div>
        <div
          v-if="renderedBody"
          ref="bodyRef"
          class="sf-content sf-content--spec prose-content"
          v-html="renderedBody"
        />
        <div v-else-if="!loading && !parsed.warning" class="sf-loading">Select a spec from the tree.</div>
      </template>
      <FloatingToc :body-el="bodyRef" :content-key="(renderedBody || renderedTaskPrompt)" />
    </div>

    <div class="sf-toasts" role="status" aria-live="polite">
      <div v-for="t in toasts" :key="t.id" class="sf-toast">
        <span class="sf-toast-text">{{ t.text }}</span>
        <button type="button" class="sf-action" @click="undoToast(t)">Undo</button>
        <button
          type="button"
          class="sf-toast-close"
          aria-label="Dismiss"
          @click="dismissToast(t.id)"
        >✕</button>
      </div>
    </div>
  </main>
  </Transition>
</template>

<style scoped>
/* Focused-view crossfade (mirrors spec-mode.js _scheduleFocusedCrossfade). */
.sf-crossfade-leave-active { transition: opacity 140ms cubic-bezier(0.3, 0, 0.8, 0.15); }
.sf-crossfade-enter-active { transition: opacity 180ms cubic-bezier(0.2, 0, 0, 1); }
.sf-crossfade-enter-from,
.sf-crossfade-leave-to { opacity: 0; }
@media (prefers-reduced-motion: reduce) {
  .sf-crossfade-leave-active,
  .sf-crossfade-enter-active { transition: none; }
}

.spec-focused {
  flex: 1;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  min-width: 0;
  position: relative;
}

.sf-header {
  padding: 12px 20px 8px;
  border-bottom: 1px solid var(--rule);
}

.sf-chrome {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 11px;
  flex-wrap: wrap;
}

.sf-path {
  font-family: var(--font-mono);
  color: var(--ink-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.sf-status,
.sf-kind {
  padding: 2px 7px;
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  background: var(--bg-card);
  color: var(--ink-2);
  font-weight: 500;
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}

/* Subtle filled tint per semantic colour — readable, less flat than a bare
   outline. */
.sf-status--validated,
.sf-status--complete {
  color: var(--ok);
  border-color: color-mix(in oklab, var(--ok) 40%, var(--rule));
  background: color-mix(in oklab, var(--ok) 12%, var(--bg-card));
}
.sf-status--drafted {
  color: var(--info);
  border-color: color-mix(in oklab, var(--info) 40%, var(--rule));
  background: color-mix(in oklab, var(--info) 12%, var(--bg-card));
}
.sf-status--stale {
  color: var(--warn);
  border-color: color-mix(in oklab, var(--warn) 40%, var(--rule));
  background: color-mix(in oklab, var(--warn) 12%, var(--bg-card));
}
.sf-status--archived { color: var(--ink-4); border-color: var(--ink-4); }
.sf-status--vague { color: var(--ink-3); border-color: var(--ink-3); }

.sf-effort {
  font-family: var(--font-mono);
  color: var(--ink-3);
}

.sf-spacer {
  flex: 1;
}

.sf-action {
  font-size: 12px;
  font-weight: 500;
  padding: 5px 12px;
  border: 1px solid var(--rule);
  border-radius: var(--r-md);
  background: var(--bg-card);
  color: var(--ink-2);
  cursor: pointer;
  white-space: nowrap;
}

.sf-action:hover:not(:disabled) {
  background: var(--bg-hover);
  border-color: color-mix(in oklab, var(--accent) 35%, var(--rule));
}

.sf-action:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px color-mix(in oklab, var(--accent) 16%, transparent);
}

.sf-action:disabled {
  opacity: 0.5;
  cursor: default;
}

.sf-dispatch {
  background: var(--accent);
  color: #fff;
  border-color: var(--accent);
}

.sf-dispatch:hover:not(:disabled) {
  background: var(--accent);
  border-color: var(--accent);
  filter: brightness(0.96);
}

.sf-chat-toggle--folded {
  opacity: 0.7;
}

.sf-title {
  display: block;
  font-family: var(--font-serif);
  font-style: italic;
  font-weight: 400;
  font-size: 36px;
  line-height: 1.1;
  letter-spacing: -0.015em;
  color: var(--ink);
  max-width: 52em;
  word-break: break-word;
}

.sf-meta {
  padding: 4px 20px 6px;
  font-size: 11px;
  color: var(--ink-3);
  border-bottom: 1px solid var(--rule);
}

.sf-archived-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 20px;
  background: var(--bg-sunk);
  font-size: 12px;
  color: var(--ink-3);
  border-bottom: 1px solid var(--rule);
}

.sf-body {
  flex: 1;
  overflow-y: auto;
  padding: 20px 28px 80px;
  animation: sf-fade-in 0.18s ease-out;
}

.sf-content--spec {
  max-width: 52em;
  font-size: 14px;
  line-height: 1.7;
}

@keyframes sf-fade-in {
  from { opacity: 0; }
  to { opacity: 1; }
}

.sf-loading {
  color: var(--ink-4);
  text-align: center;
  padding: 40px 0;
  font-size: 13px;
}
.sf-dispatched-pill {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  background: var(--accent-tint, transparent);
  border: 1px solid var(--accent);
  color: var(--accent);
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 11px;
  font-family: var(--font-mono);
  cursor: pointer;
}
.sf-dispatched-pill:hover { background: var(--accent); color: #fff; }
.sf-frontmatter-warning {
  margin: 0 0 12px;
  padding: 8px 12px;
  background: color-mix(in oklab, var(--warn, #c87b1c) 18%, var(--bg-card));
  border: 1px solid color-mix(in oklab, var(--warn, #c87b1c) 35%, var(--border));
  border-radius: 6px;
  font-size: 12px;
  color: var(--ink);
}

.sf-content :deep(h1),
.sf-content :deep(h2),
.sf-content :deep(h3),
.sf-content :deep(h4) {
  margin-top: 1.4em;
  margin-bottom: 0.5em;
  line-height: 1.3;
}

.sf-content :deep(h1) { font-size: 22px; }
.sf-content :deep(h2) { font-size: 17px; border-bottom: 1px solid var(--rule); padding-bottom: 4px; }
.sf-content :deep(h3) { font-size: 14px; }

/* Spec body reading scale — larger headings and a drop-cap on the first
   paragraph (mirrors golden .spec-focused-view__body-inner). Gated to spec
   bodies so the task-prompt body keeps the compact scale above. */
.sf-content--spec.prose-content :deep(h2) {
  font-size: 20px;
  border-bottom: none;
  padding-bottom: 0;
  margin: 28px 0 10px;
}
.sf-content--spec.prose-content :deep(h3) {
  font-size: 15px;
  color: var(--ink);
  margin: 22px 0 8px;
}
/* Direct-child combinator so the drop cap only hits the lead paragraph, not
   the first <p> nested inside every <li> of a loose list. */
.sf-content--spec.prose-content :deep(> p:first-of-type::first-letter) {
  font-family: var(--font-serif);
  font-style: italic;
  font-size: 3em;
  line-height: 0.9;
  float: left;
  padding: 4px 10px 0 0;
  color: var(--accent);
}
.sf-content--spec.prose-content :deep(> p:first-of-type) {
  font-size: 15px;
  line-height: 1.72;
}

.sf-content :deep(p) { margin: 0.6em 0; line-height: 1.6; }
.sf-content :deep(ul),
.sf-content :deep(ol) { margin: 0.6em 0; padding-left: 1.4em; }
.sf-content :deep(li) { margin: 0.2em 0; line-height: 1.55; }
.sf-content :deep(code) {
  font-family: var(--font-mono);
  font-size: 0.92em;
  background: var(--bg-sunk);
  border: 1px solid color-mix(in oklab, var(--rule) 60%, transparent);
  padding: 1px 5px;
  border-radius: 4px;
}
.sf-content :deep(pre) {
  font-family: var(--font-mono);
  font-size: 12px;
  background: var(--bg-sunk);
  border: 1px solid var(--rule);
  padding: 12px 14px;
  border-radius: var(--r-md);
  overflow-x: auto;
  line-height: 1.5;
}
.sf-content :deep(pre code) {
  background: transparent;
  border: none;
  padding: 0;
}
.sf-content :deep(a) {
  color: var(--accent);
  text-decoration: none;
}
.sf-content :deep(a:hover) {
  text-decoration: underline;
}
.sf-content :deep(blockquote) {
  border-left: 3px solid var(--rule);
  padding-left: 12px;
  margin: 1em 0;
  color: var(--ink-3);
}
.sf-content :deep(table) {
  border-collapse: collapse;
  margin: 1em 0;
  font-size: 12px;
}
.sf-content :deep(th),
.sf-content :deep(td) {
  border: 1px solid var(--rule);
  padding: 4px 8px;
  text-align: left;
}

.sf-toasts {
  position: absolute;
  bottom: 16px;
  right: 20px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  pointer-events: none;
}

.sf-toast {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
  font-size: 12px;
  pointer-events: auto;
}

.sf-toast-text {
  flex: 1;
}

.sf-toast-close {
  background: none;
  border: none;
  color: var(--ink-3);
  cursor: pointer;
  font-size: 12px;
  padding: 2px 4px;
}
</style>
