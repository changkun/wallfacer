<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted, computed, nextTick } from 'vue';
import { useRouter } from 'vue-router';
import { api, withAuthToken } from '../api/client';
import { useTaskStore } from '../stores/tasks';
import { useDialogStore } from '../stores/dialog';
import hljs from 'highlight.js/lib/common';
import { renderMarkdown } from '../lib/markdown';
import { extToLang, splitHighlightedLines } from '../lib/diffHighlight';
import { mapEntries, type RawExplorerEntry, type TreeEntry } from '../lib/explorerTree';
import { fileIcon, type FileIcon } from '../lib/fileIcon';

// Collapsible file-explorer side panel. Lives inside BoardPage to the left of
// the board grid (see specs/foundations/file-explorer.md) so browsing files
// never hides the board. File preview opens in a modal because the board grid
// occupies the space an inline preview pane would have used.
const emit = defineEmits<{ close: [] }>();

interface TaskPromptEntry {
  task_id: string;
  title: string;
  status: string;
  updated_at: string;
}

const store = useTaskStore();
const router = useRouter();
const dialog = useDialogStore();

// Task Prompts virtual section — backlog (+ waiting when toggled on)
// tasks rendered as clickable entries above the regular file tree. The
// list reloads on any task SSE event so it stays in sync with the board.
const taskPrompts = ref<TaskPromptEntry[]>([]);
const taskPromptsExpanded = ref(true);
const taskPromptsIncludeWaiting = ref(false);

async function loadTaskPrompts() {
  try {
    const status = taskPromptsIncludeWaiting.value ? 'backlog,waiting' : 'backlog';
    taskPrompts.value = await api<TaskPromptEntry[]>(
      'GET',
      `/api/explorer/task-prompts?status=${encodeURIComponent(status)}`,
    );
  } catch {
    // section is optional; failure is silent
  }
}

function openTaskPrompt(entry: TaskPromptEntry) {
  // Deep-link via the hash route handler in App.vue. The panel already lives
  // on the board route, so this just opens the task detail overlay.
  void router.push({ path: '/', hash: `#${entry.task_id}` });
}

const children = ref<Map<string, TreeEntry[]>>(new Map());
const expanded = ref<Set<string>>(new Set());

// Semantic file-type icon (colour + SVG paths) for a tree entry. See lib/fileIcon.
function iconFor(entry: TreeEntry): FileIcon {
  return fileIcon(entry.name, entry.is_dir, expanded.value.has(entry.path));
}
const selectedPath = ref<string | null>(null);
const fileContent = ref<string | null>(null);
const fileLoading = ref(false);
const treeLoading = ref(true);
const errorMsg = ref('');
// Edit mode.
const editing = ref(false);
const editBuffer = ref('');
const saving = ref(false);
const saveError = ref('');

// Drag-resizable panel width, persisted to localStorage. Clamp lives between
// the min and 50vw; explorer.css owns the matching min/max.
const EXPLORER_DEFAULT_WIDTH = 260;
const EXPLORER_MIN_WIDTH = 200;
const EXPLORER_WIDTH_KEY = 'wallfacer-explorer-width';
const panelWidth = ref(EXPLORER_DEFAULT_WIDTH);
let resizeStartX = 0;
let resizeStartW = 0;
const resizing = ref(false);

function maxPanelWidth(): number {
  return Math.floor(window.innerWidth * 0.5);
}

function onResizeMove(e: PointerEvent) {
  const delta = e.clientX - resizeStartX;
  panelWidth.value = Math.min(maxPanelWidth(), Math.max(EXPLORER_MIN_WIDTH, resizeStartW + delta));
}

function onResizeEnd() {
  resizing.value = false;
  window.removeEventListener('pointermove', onResizeMove);
  window.removeEventListener('pointerup', onResizeEnd);
  document.body.style.userSelect = '';
  document.body.style.cursor = '';
  localStorage.setItem(EXPLORER_WIDTH_KEY, String(panelWidth.value));
}

function onResizeStart(e: PointerEvent) {
  e.preventDefault();
  resizeStartX = e.clientX;
  resizeStartW = panelWidth.value;
  resizing.value = true;
  document.body.style.userSelect = 'none';
  document.body.style.cursor = 'col-resize';
  window.addEventListener('pointermove', onResizeMove);
  window.addEventListener('pointerup', onResizeEnd);
}

function resetWidth() {
  panelWidth.value = EXPLORER_DEFAULT_WIDTH;
  localStorage.setItem(EXPLORER_WIDTH_KEY, String(EXPLORER_DEFAULT_WIDTH));
}

function workspace(): string {
  return store.config?.workspaces?.[0] ?? '';
}

async function fetchChildren(dirPath: string) {
  const ws = workspace();
  if (!ws) return;
  // The backend requires a non-empty `path`; the root level lists the
  // workspace directory itself. The map stays keyed by `dirPath` ('' for
  // root) so visibleEntries()'s walk('') and toggleDir(absolutePath) both
  // resolve consistently.
  const reqPath = dirPath || ws;
  try {
    const url = `/api/explorer/tree?workspace=${encodeURIComponent(ws)}&path=${encodeURIComponent(reqPath)}`;
    // The endpoint returns a bare JSON array of entries, not {entries:[…]}.
    const res = await api<RawExplorerEntry[]>('GET', url);
    children.value.set(dirPath, mapEntries(reqPath, Array.isArray(res) ? res : []));
    if (!dirPath) errorMsg.value = '';
  } catch (e) {
    console.error('explorer tree:', e);
    if (!dirPath) errorMsg.value = 'Failed to load file tree.';
  }
}

async function loadRoot() {
  treeLoading.value = true;
  errorMsg.value = '';
  children.value = new Map();
  expanded.value = new Set();
  selectedPath.value = null;
  fileContent.value = null;
  await fetchChildren('');
  treeLoading.value = false;
}

async function toggleDir(entry: TreeEntry) {
  const path = entry.path;
  if (expanded.value.has(path)) {
    expanded.value.delete(path);
  } else {
    expanded.value.add(path);
    if (!children.value.has(path)) {
      await fetchChildren(path);
    }
  }
}

function startEdit() {
  editBuffer.value = fileContent.value ?? '';
  saveError.value = '';
  editing.value = true;
}

async function cancelEdit() {
  // Guard against discarding unsaved edits, matching the legacy dirty-close.
  if (editBuffer.value !== (fileContent.value ?? '')) {
    const ok = await dialog.confirm({
      title: 'Discard changes?',
      message: 'You have unsaved edits to this file. Discard them?',
      confirmLabel: 'Discard',
      cancelLabel: 'Keep editing',
      danger: true,
    });
    if (!ok) return;
  }
  editing.value = false;
  saveError.value = '';
}

// Tab inserts two spaces in the editor instead of moving focus away.
function onEditKeydown(e: KeyboardEvent) {
  if (e.key !== 'Tab') return;
  e.preventDefault();
  const ta = e.target as HTMLTextAreaElement;
  const start = ta.selectionStart ?? 0;
  const end = ta.selectionEnd ?? 0;
  editBuffer.value = editBuffer.value.slice(0, start) + '  ' + editBuffer.value.slice(end);
  void nextTick(() => { ta.selectionStart = ta.selectionEnd = start + 2; });
}

// Absolute locale date for the Task Prompts updated_at column.
function entryDate(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString();
}

async function saveFile() {
  const ws = workspace();
  if (!ws || !selectedPath.value || saving.value) return;
  saving.value = true;
  saveError.value = '';
  try {
    await api('PUT', '/api/explorer/file', {
      workspace: ws,
      path: selectedPath.value,
      content: editBuffer.value,
    });
    fileContent.value = editBuffer.value;
    editing.value = false;
  } catch (e: unknown) {
    saveError.value = e instanceof Error ? e.message : 'Failed to save file.';
  } finally {
    saving.value = false;
  }
}

// Keyboard nav over the explorer tree. The visible-entries list is
// already a flat preorder traversal, so up/down on the focused row
// just moves through that list; right expands a closed dir or moves
// into the first child of an open one; left collapses an open dir or
// moves to the parent.
function visibleIndexOf(path: string | null): number {
  if (path == null) return -1;
  const list = visibleEntries();
  return list.findIndex((v) => v.entry.path === path);
}

async function onTreeKeydown(e: KeyboardEvent, entry: TreeEntry) {
  const list = visibleEntries();
  const idx = visibleIndexOf(entry.path);
  if (idx < 0) return;
  switch (e.key) {
    case 'ArrowDown': {
      e.preventDefault();
      const next = list[Math.min(list.length - 1, idx + 1)];
      if (next) focusTreeRow(next.entry.path);
      return;
    }
    case 'ArrowUp': {
      e.preventDefault();
      const prev = list[Math.max(0, idx - 1)];
      if (prev) focusTreeRow(prev.entry.path);
      return;
    }
    case 'ArrowRight': {
      e.preventDefault();
      if (entry.is_dir) {
        if (!expanded.value.has(entry.path)) {
          await toggleDir(entry);
        } else {
          const next = list[idx + 1];
          if (next) focusTreeRow(next.entry.path);
        }
      }
      return;
    }
    case 'ArrowLeft': {
      e.preventDefault();
      if (entry.is_dir && expanded.value.has(entry.path)) {
        await toggleDir(entry); // collapse
      } else {
        // Move to parent if visible.
        const parent = entry.path.split('/').slice(0, -1).join('/');
        focusTreeRow(parent);
      }
      return;
    }
    case 'Enter':
    case ' ': {
      e.preventDefault();
      if (entry.is_dir) await toggleDir(entry);
      else await selectFile(entry);
      return;
    }
  }
}

function focusTreeRow(path: string) {
  // Re-query because v-for may have re-rendered after a toggleDir().
  requestAnimationFrame(() => {
    const el = document.querySelector<HTMLElement>(
      `.explorer-node[data-path="${CSS.escape(path)}"]`,
    );
    el?.focus();
  });
}

async function selectFile(entry: TreeEntry) {
  const ws = workspace();
  if (!ws) return;
  selectedPath.value = entry.path;
  fileContent.value = null;
  editing.value = false;
  saveError.value = '';
  fileLoading.value = true;
  try {
    const url = `/api/explorer/file?workspace=${encodeURIComponent(ws)}&path=${encodeURIComponent(entry.path)}`;
    const res = await api<{ content: string }>('GET', url);
    fileContent.value = typeof res === 'string' ? res : (res.content ?? JSON.stringify(res, null, 2));
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : 'Failed to load file.';
    fileContent.value = `Error: ${msg}`;
  }
  fileLoading.value = false;
}

function visibleEntries(): { entry: TreeEntry; depth: number }[] {
  const result: { entry: TreeEntry; depth: number }[] = [];
  function walk(parentPath: string, depth: number) {
    const entries = children.value.get(parentPath);
    if (!entries) return;
    for (const entry of entries) {
      result.push({ entry, depth });
      if (entry.is_dir && expanded.value.has(entry.path)) {
        walk(entry.path, depth + 1);
      }
    }
  }
  walk('', 0);
  return result;
}

function fileName(path: string): string {
  const parts = path.split('/');
  return parts[parts.length - 1] || path;
}

// Syntax-highlighted source lines (ported from the old _renderHighlightedContent):
// explicit language when known, hljs auto-detect otherwise, plain escape on
// failure. Each entry is per-line hljs HTML rendered via v-html in __lc.
function highlightCode(content: string, filename: string): string[] {
  const lang = extToLang(filename);
  let highlighted: string;
  try {
    highlighted = lang
      ? hljs.highlight(content, { language: lang }).value
      : hljs.highlightAuto(content).value;
  } catch {
    highlighted = escapeHtml(content);
  }
  return splitHighlightedLines(highlighted);
}

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

const previewLines = computed<string[]>(() =>
  fileContent.value == null ? [] : highlightCode(fileContent.value, fileName(selectedPath.value ?? '')),
);

async function closePreview() {
  if (editing.value) {
    // Honour the dirty-edit guard before tearing the modal down.
    await cancelEdit();
    if (editing.value) return;
  }
  selectedPath.value = null;
  fileContent.value = null;
}

// Markdown preview: rendered by default for .md / .markdown files; the
// user can switch back to the line-numbered source view via a toolbar
// button. Edit mode always shows the raw textarea regardless.
const isMarkdownFile = computed(() => /\.(md|markdown)$/i.test(selectedPath.value ?? ''));
const previewMode = ref<'rendered' | 'source'>('rendered');
const renderedHtml = computed(() =>
  isMarkdownFile.value && fileContent.value ? renderMarkdown(fileContent.value) : '',
);
watch(selectedPath, () => { previewMode.value = 'rendered'; });

// Escape closes the preview modal first, then the panel.
function onKeydown(e: KeyboardEvent) {
  if (e.key !== 'Escape') return;
  if (selectedPath.value) { void closePreview(); return; }
  emit('close');
}

// Live tree refresh: subscribes to /api/explorer/stream and re-fetches the
// affected directories whenever the server detects a content change (3 s
// poll, fingerprint-based). The stream auto-reconnects via EventSource's
// default behavior; we just close it on unmount.
let explorerStream: EventSource | null = null;
function startExplorerStream() {
  if (typeof EventSource === 'undefined') return;
  explorerStream?.close();
  const url = withAuthToken('/api/explorer/stream');
  explorerStream = new EventSource(url);
  explorerStream.addEventListener('refresh', async () => {
    // Re-fetch the root + every currently-expanded directory. Children are
    // keyed by path; collapsed nodes intentionally stay stale until the
    // user expands them again.
    await fetchChildren('');
    for (const p of expanded.value) await fetchChildren(p);
  });
}

onMounted(async () => {
  const stored = parseInt(localStorage.getItem(EXPLORER_WIDTH_KEY) ?? '', 10);
  if (stored >= EXPLORER_MIN_WIDTH) panelWidth.value = stored;
  if (!store.config) await store.fetchConfig();
  if (workspace()) await loadRoot();
  else treeLoading.value = false;
  startExplorerStream();
  await loadTaskPrompts();
  window.addEventListener('keydown', onKeydown);
});

// Refresh the Task Prompts virtual section whenever the global task list
// changes. Watching store.tasks.length is cheap and catches the snapshot
// + per-task SSE deltas the AppLayout subscribes to.
watch(() => store.tasks.length, () => { void loadTaskPrompts(); });
watch(taskPromptsIncludeWaiting, () => { void loadTaskPrompts(); });

onUnmounted(() => {
  explorerStream?.close();
  window.removeEventListener('keydown', onKeydown);
  window.removeEventListener('pointermove', onResizeMove);
  window.removeEventListener('pointerup', onResizeEnd);
});

watch(() => store.config?.workspaces?.[0], (ws) => {
  if (ws) loadRoot();
});
</script>

<template>
  <aside class="explorer-panel" :style="{ width: panelWidth + 'px' }">
    <div class="explorer-panel__header">
      <span class="explorer-panel__title">Explorer</span>
      <button
        type="button"
        class="explorer-panel__close"
        title="Close explorer"
        aria-label="Close explorer"
        @click="emit('close')"
      >&times;</button>
    </div>
    <div v-if="taskPrompts.length" class="explorer-task-prompts">
      <div
        class="explorer-task-prompts__header"
        role="button"
        tabindex="0"
        :aria-expanded="taskPromptsExpanded"
        @click="taskPromptsExpanded = !taskPromptsExpanded"
        @keydown.enter.prevent="taskPromptsExpanded = !taskPromptsExpanded"
        @keydown.space.prevent="taskPromptsExpanded = !taskPromptsExpanded"
      >
        <span class="explorer-node__toggle">{{ taskPromptsExpanded ? '▼' : '▶' }}</span>
        <span class="explorer-task-prompts__label">Task Prompts</span>
        <button
          type="button"
          class="explorer-task-prompts__waiting-toggle"
          :title="taskPromptsIncludeWaiting ? 'Hide waiting tasks' : 'Show waiting tasks'"
          :aria-pressed="taskPromptsIncludeWaiting"
          @click.stop="taskPromptsIncludeWaiting = !taskPromptsIncludeWaiting"
        >{{ taskPromptsIncludeWaiting ? 'W' : 'w' }}</button>
      </div>
      <div v-if="taskPromptsExpanded">
        <button
          v-for="entry in taskPrompts"
          :key="entry.task_id"
          type="button"
          class="explorer-task-prompts__entry"
          :title="entry.title"
          @click="openTaskPrompt(entry)"
        >
          <span class="explorer-task-prompts__badge" :class="`explorer-task-prompts__badge--${entry.status}`">{{ entry.status }}</span>
          <span class="explorer-task-prompts__title">{{ entry.title || entry.task_id.slice(0, 8) }}</span>
          <span v-if="entry.updated_at" class="explorer-task-prompts__time">{{ entryDate(entry.updated_at) }}</span>
        </button>
      </div>
    </div>
    <div class="explorer-panel__tree">
      <div v-if="treeLoading" class="explorer-panel__empty">Loading...</div>
      <div v-else-if="errorMsg" class="explorer-panel__empty explorer-panel__empty--error">{{ errorMsg }}</div>
      <div v-else-if="!children.get('')?.length" class="explorer-panel__empty">No files found.</div>
      <template v-else>
        <div
          v-for="{ entry, depth } in visibleEntries()"
          :key="entry.path"
          class="explorer-node"
          :class="[
            entry.is_dir ? 'explorer-node--dir' : 'explorer-node--file',
            { 'explorer-node--active': selectedPath === entry.path },
          ]"
          :style="{ paddingLeft: (8 + depth * 14) + 'px' }"
          :data-path="entry.path"
          tabindex="0"
          role="treeitem"
          :aria-expanded="entry.is_dir ? expanded.has(entry.path) : undefined"
          @click="entry.is_dir ? toggleDir(entry) : selectFile(entry)"
          @keydown="(e) => onTreeKeydown(e, entry)"
        >
          <span class="explorer-node__toggle">
            <template v-if="entry.is_dir">{{ expanded.has(entry.path) ? '▼' : '▶' }}</template>
          </span>
          <span class="explorer-node__icon" aria-hidden="true">
            <svg
              width="14"
              height="14"
              viewBox="0 0 24 24"
              fill="none"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
              :stroke="iconFor(entry).color"
              v-html="iconFor(entry).paths"
            ></svg>
          </span>
          <span class="explorer-node__name">{{ entry.name }}</span>
        </div>
      </template>
    </div>
    <div
      class="explorer-panel__resize-handle"
      :class="{ 'explorer-panel__resize-handle--active': resizing }"
      title="Drag to resize, double-click to reset"
      @pointerdown="onResizeStart"
      @dblclick="resetWidth"
    ></div>
  </aside>

  <!-- File preview modal: the board grid occupies the space an inline pane
       would use, so previews open over the board (legacy modal styles). -->
  <div v-if="selectedPath" class="explorer-preview-backdrop" @click.self="closePreview">
    <div class="explorer-preview" role="dialog" aria-modal="true">
      <div class="explorer-preview__header">
        <span class="explorer-preview__path" :title="selectedPath">
          {{ fileName(selectedPath) }}
        </span>
        <span class="explorer-preview__actions">
          <span v-if="saveError" class="explorer-save-error" :title="saveError">save failed</span>
          <button
            v-if="!editing && isMarkdownFile"
            type="button"
            class="explorer-preview__edit-btn"
            @click="previewMode = previewMode === 'rendered' ? 'source' : 'rendered'"
          >{{ previewMode === 'rendered' ? 'Raw' : 'Preview' }}</button>
          <template v-if="editing">
            <button type="button" class="explorer-preview__save-btn" :disabled="saving" @click="saveFile">{{ saving ? 'Saving…' : 'Save' }}</button>
            <button type="button" class="explorer-preview__discard-btn" :disabled="saving" @click="cancelEdit">Discard</button>
          </template>
          <button v-else type="button" class="explorer-preview__edit-btn" @click="startEdit">Edit</button>
          <button type="button" class="explorer-preview__close" title="Close" aria-label="Close preview" @click="closePreview">&times;</button>
        </span>
      </div>
      <div class="explorer-preview__content">
        <div v-if="fileLoading" class="explorer-preview__placeholder">Loading...</div>
        <textarea
          v-else-if="editing"
          v-model="editBuffer"
          class="explorer-preview__textarea"
          spellcheck="false"
          @keydown="onEditKeydown"
        ></textarea>
        <!-- eslint-disable-next-line vue/no-v-html — renderMarkdown sanitises -->
        <div
          v-else-if="isMarkdownFile && previewMode === 'rendered'"
          class="explorer-preview__markdown"
          v-html="renderedHtml"
        />
        <!-- eslint-disable-next-line vue/no-v-html — hljs token spans only -->
        <pre v-else class="explorer-preview__code"><code><span
            v-for="(line, idx) in previewLines"
            :key="idx"
            class="explorer-preview__line"
          ><span class="explorer-preview__ln">{{ idx + 1 }}</span><span class="explorer-preview__lc" v-html="line || ' '"></span></span></code></pre>
      </div>
    </div>
  </div>
</template>

<style scoped>
.explorer-panel__header { justify-content: space-between; }
.explorer-panel__close {
  background: none;
  border: none;
  cursor: pointer;
  font-size: 18px;
  line-height: 1;
  padding: 0 2px;
  color: var(--text-muted);
}
.explorer-panel__close:hover { color: var(--text); }
.explorer-panel__empty {
  padding: 12px 8px;
  font-size: 12px;
  color: var(--text-muted);
}
.explorer-panel__empty--error { color: var(--err, #c0392b); }
</style>
