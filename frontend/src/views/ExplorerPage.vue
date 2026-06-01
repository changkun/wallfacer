<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted, computed, nextTick } from 'vue';
import { useRouter } from 'vue-router';
import { api } from '../api/client';
import { useTaskStore } from '../stores/tasks';
import { useDialogStore } from '../stores/dialog';
import { renderMarkdown } from '../lib/markdown';
import { mapEntries, type RawExplorerEntry, type TreeEntry } from '../lib/explorerTree';
import { fileIcon, type FileIcon } from '../lib/fileIcon';

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
  // Deep-link via the hash route handler in App.vue.
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

// Relative "x ago" for the Task Prompts updated_at column.
function relAgo(iso: string): string {
  if (!iso) return '';
  const ms = Date.now() - new Date(iso).getTime();
  if (Number.isNaN(ms)) return '';
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
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

function previewLines(): string[] {
  if (fileContent.value == null) return [];
  return fileContent.value.split('\n');
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

// Live tree refresh: subscribes to /api/explorer/stream and re-fetches the
// affected directories whenever the server detects a content change (3 s
// poll, fingerprint-based). The stream auto-reconnects via EventSource's
// default behavior; we just close it on unmount.
let explorerStream: EventSource | null = null;
function startExplorerStream() {
  if (typeof EventSource === 'undefined') return;
  explorerStream?.close();
  let url = '/api/explorer/stream';
  const key = window.__WALLFACER__?.serverApiKey;
  if (key) url += `?token=${encodeURIComponent(key)}`;
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
  if (!store.config) await store.fetchConfig();
  if (workspace()) await loadRoot();
  startExplorerStream();
  await loadTaskPrompts();
});

// Refresh the Task Prompts virtual section whenever the global task list
// changes. Watching store.tasks.length is cheap and catches the snapshot
// + per-task SSE deltas the AppLayout subscribes to.
watch(() => store.tasks.length, () => { void loadTaskPrompts(); });
watch(taskPromptsIncludeWaiting, () => { void loadTaskPrompts(); });

onUnmounted(() => { explorerStream?.close(); });

watch(() => store.config?.workspaces?.[0], (ws) => {
  if (ws) loadRoot();
});
</script>

<template>
  <div class="board-with-explorer explorer-page-root">
    <aside class="explorer-panel">
      <div class="explorer-panel__header">
        <span class="explorer-panel__title">Explorer</span>
      </div>
      <div v-if="taskPrompts.length" class="explorer-task-prompts">
        <button
          type="button"
          class="explorer-task-prompts__header"
          :aria-expanded="taskPromptsExpanded"
          @click="taskPromptsExpanded = !taskPromptsExpanded"
        >
          <span>{{ taskPromptsExpanded ? '▼' : '▶' }}</span>
          <span>Task Prompts</span>
          <span class="explorer-task-prompts__count">{{ taskPrompts.length }}</span>
        </button>
        <div v-if="taskPromptsExpanded">
          <label class="explorer-task-prompts__toggle">
            <input
              v-model="taskPromptsIncludeWaiting"
              type="checkbox"
            />
            Include waiting
          </label>
          <button
            v-for="entry in taskPrompts"
            :key="entry.task_id"
            type="button"
            class="explorer-task-prompts__item"
            :title="entry.title"
            @click="openTaskPrompt(entry)"
          >
            <span class="explorer-task-prompts__badge" :data-status="entry.status">{{ entry.status }}</span>
            <span class="explorer-task-prompts__title">{{ entry.title || entry.task_id.slice(0, 8) }}</span>
            <span v-if="entry.updated_at" class="explorer-task-prompts__time">{{ relAgo(entry.updated_at) }}</span>
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
    </aside>

    <section class="explorer-content-pane">
      <div v-if="fileLoading" class="explorer-preview__placeholder">Loading...</div>
      <div v-else-if="!selectedPath" class="explorer-preview__placeholder">
        Select a file to view its contents.
      </div>
      <template v-else>
        <div class="explorer-preview__header">
          <span class="explorer-preview__path" :title="selectedPath">
            {{ fileName(selectedPath) }}
          </span>
          <span class="explorer-preview__actions">
            <span v-if="saveError" class="explorer-save-error" :title="saveError">save failed</span>
            <button
              v-if="!editing && isMarkdownFile"
              type="button"
              class="explorer-edit-btn"
              @click="previewMode = previewMode === 'rendered' ? 'source' : 'rendered'"
            >{{ previewMode === 'rendered' ? 'Source' : 'Render' }}</button>
            <template v-if="editing">
              <button type="button" class="explorer-edit-btn" :disabled="saving" @click="saveFile">{{ saving ? 'Saving…' : 'Save' }}</button>
              <button type="button" class="explorer-edit-btn" :disabled="saving" @click="cancelEdit">Cancel</button>
            </template>
            <button v-else type="button" class="explorer-edit-btn" @click="startEdit">Edit</button>
          </span>
        </div>
        <div class="explorer-preview__content">
          <textarea
            v-if="editing"
            v-model="editBuffer"
            class="explorer-edit-area"
            spellcheck="false"
            @keydown="onEditKeydown"
          ></textarea>
          <!-- eslint-disable-next-line vue/no-v-html — renderMarkdown sanitises -->
          <div
            v-else-if="isMarkdownFile && previewMode === 'rendered'"
            class="explorer-preview__md prose"
            v-html="renderedHtml"
          />
          <pre v-else class="explorer-preview__code"><code>
            <div
              v-for="(line, idx) in previewLines()"
              :key="idx"
              class="explorer-preview__line"
            ><span class="explorer-preview__ln">{{ idx + 1 }}</span><span class="explorer-preview__lc">{{ line }}</span></div>
          </code></pre>
        </div>
      </template>
    </section>
  </div>
</template>

<style scoped>
.explorer-page-root {
  height: 100%;
  overflow: hidden;
}
.explorer-page-root :deep(.explorer-panel) {
  width: 280px;
  min-width: 220px;
}
.explorer-content-pane {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  background: var(--bg);
}
.explorer-task-prompts {
  border-bottom: 1px solid var(--rule);
  padding: 4px 6px 6px;
  font-size: 11px;
}
.explorer-task-prompts__header {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  background: transparent;
  border: none;
  padding: 4px 4px;
  cursor: pointer;
  color: var(--ink-2);
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  font-size: 10px;
  text-align: left;
}
.explorer-task-prompts__count {
  margin-left: auto;
  background: var(--bg-sunk);
  color: var(--ink-3);
  padding: 0 5px;
  border-radius: 999px;
}
.explorer-task-prompts__toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 2px 4px 6px;
  color: var(--ink-3);
  cursor: pointer;
}
.explorer-task-prompts__item {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  background: transparent;
  border: none;
  padding: 4px 4px;
  cursor: pointer;
  color: var(--ink-2);
  text-align: left;
}
.explorer-task-prompts__item:hover { background: var(--bg-hover); }
.explorer-task-prompts__badge {
  font-size: 9px;
  text-transform: uppercase;
  padding: 1px 5px;
  border-radius: 4px;
  background: var(--bg-sunk);
  color: var(--ink-3);
  flex-shrink: 0;
}
.explorer-task-prompts__badge[data-status="waiting"] { color: var(--warn, #c87b1c); }
.explorer-task-prompts__time {
  flex-shrink: 0;
  color: var(--ink-4);
  font-size: 9px;
  font-variant-numeric: tabular-nums;
}
.explorer-task-prompts__title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
