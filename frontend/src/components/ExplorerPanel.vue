<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted } from 'vue';
import { useRouter } from 'vue-router';
import { api, withAuthToken } from '../api/client';
import { useTaskStore } from '../stores/tasks';
import { useEditorTabsStore } from '../stores/editorTabs';
import { mapEntries, type RawExplorerEntry, type TreeEntry } from '../lib/explorerTree';
import { fileIcon, type FileIcon } from '../lib/fileIcon';

// Collapsible file-explorer side panel. Lives inside BoardPage to the left of
// the board grid (see specs/foundations/file-explorer.md) so browsing files
// never hides the board. Clicking a file opens it as an editor tab (see
// stores/editorTabs); the board's top-bar tab strip swaps the center pane.
const emit = defineEmits<{ close: [] }>();

interface TaskPromptEntry {
  task_id: string;
  title: string;
  status: string;
  updated_at: string;
}

const store = useTaskStore();
const router = useRouter();
const editorTabs = useEditorTabsStore();

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
const treeLoading = ref(true);
const errorMsg = ref('');

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

// Absolute locale date for the Task Prompts updated_at column.
function entryDate(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? '' : d.toLocaleDateString();
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

function selectFile(entry: TreeEntry) {
  const ws = workspace();
  if (!ws) return;
  // Single-click opens (or focuses) the file as a preview tab; the board's tab
  // strip swaps the center pane to show it. The store fetches and holds content.
  void editorTabs.openFile(ws, entry.path);
}

// Double-click opens the file as a permanent (kept) tab, like VS Code.
function openFilePinned(entry: TreeEntry) {
  const ws = workspace();
  if (ws) void editorTabs.openFile(ws, entry.path, { preview: false });
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

// Escape closes the explorer panel (files now live in editor tabs, not a modal).
function onKeydown(e: KeyboardEvent) {
  if (e.key !== 'Escape') return;
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
  // Send the currently-expanded directory paths so the server fingerprints
  // them too, not just the workspace roots. Without this, content edits and
  // changes more than one level deep never trigger a refresh.
  const paths = [...expanded.value].join(',');
  const base = `/api/explorer/stream${paths ? `?paths=${encodeURIComponent(paths)}` : ''}`;
  explorerStream = new EventSource(withAuthToken(base));
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

// Re-open the stream when the expanded set changes so the server fingerprints
// the newly visible directories (an EventSource URL is fixed once opened).
watch(() => [...expanded.value].sort().join(','), () => {
  if (explorerStream) startExplorerStream();
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
            { 'explorer-node--active': editorTabs.activeId === entry.path },
          ]"
          :style="{ paddingLeft: (8 + depth * 14) + 'px' }"
          :data-path="entry.path"
          tabindex="0"
          role="treeitem"
          :aria-expanded="entry.is_dir ? expanded.has(entry.path) : undefined"
          @click="entry.is_dir ? toggleDir(entry) : selectFile(entry)"
          @dblclick="!entry.is_dir && openFilePinned(entry)"
          @keydown="(e) => onTreeKeydown(e, entry)"
        >
          <span
            class="explorer-node__toggle"
            :class="{ 'is-dir': entry.is_dir, 'is-open': entry.is_dir && expanded.has(entry.path) }"
            aria-hidden="true"
          ></span>
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
