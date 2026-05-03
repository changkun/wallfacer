<script setup lang="ts">
import { ref, watch, onMounted } from 'vue';
import { api } from '../api/client';
import { useTaskStore } from '../stores/tasks';

interface TreeEntry {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
}

const store = useTaskStore();

const children = ref<Map<string, TreeEntry[]>>(new Map());
const expanded = ref<Set<string>>(new Set());
const selectedPath = ref<string | null>(null);
const fileContent = ref<string | null>(null);
const fileLoading = ref(false);
const treeLoading = ref(true);
const errorMsg = ref('');

function workspace(): string {
  return store.config?.workspaces?.[0] ?? '';
}

async function fetchChildren(dirPath: string) {
  const ws = workspace();
  if (!ws) return;
  try {
    let url = `/api/explorer/tree?workspace=${encodeURIComponent(ws)}`;
    if (dirPath) {
      url += `&path=${encodeURIComponent(dirPath)}`;
    }
    const res = await api<{ entries: TreeEntry[] }>('GET', url);
    const sorted = (res.entries ?? []).slice().sort((a, b) => {
      if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
      return a.name.localeCompare(b.name);
    });
    children.value.set(dirPath, sorted);
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

async function selectFile(entry: TreeEntry) {
  const ws = workspace();
  if (!ws) return;
  selectedPath.value = entry.path;
  fileContent.value = null;
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

onMounted(async () => {
  if (!store.config) await store.fetchConfig();
  if (workspace()) await loadRoot();
});

watch(() => store.config?.workspaces?.[0], (ws) => {
  if (ws) loadRoot();
});
</script>

<template>
  <div class="explorer-page">
    <header class="page-header">
      <h1>Explorer</h1>
    </header>

    <div class="explorer-layout">
      <div class="tree-pane">
        <div v-if="treeLoading" class="tree-empty">Loading...</div>
        <div v-else-if="errorMsg" class="tree-empty tree-error">{{ errorMsg }}</div>
        <div v-else-if="!children.get('')?.length" class="tree-empty">No files found.</div>
        <div v-else class="tree-list">
          <div
            v-for="{ entry, depth } in visibleEntries()"
            :key="entry.path"
            class="tree-row"
            :class="{ selected: selectedPath === entry.path }"
            :style="{ paddingLeft: (12 + depth * 16) + 'px' }"
            @click="entry.is_dir ? toggleDir(entry) : selectFile(entry)"
          >
            <span v-if="entry.is_dir" class="tree-icon dir-icon">
              {{ expanded.has(entry.path) ? '▼' : '▶' }}
            </span>
            <span v-else class="tree-icon file-icon">─</span>
            <span class="tree-name">{{ entry.name }}</span>
          </div>
        </div>
      </div>

      <div class="content-pane">
        <div v-if="fileLoading" class="content-empty">Loading...</div>
        <div v-else-if="!selectedPath" class="content-empty">Select a file to view its contents.</div>
        <div v-else class="content-view">
          <div class="content-header">
            <span class="content-path">{{ fileName(selectedPath) }}</span>
            <span class="content-fullpath">{{ selectedPath }}</span>
          </div>
          <pre class="content-code">{{ fileContent }}</pre>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.explorer-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
.page-header {
  padding: 12px 20px;
  border-bottom: 1px solid var(--rule);
  flex-shrink: 0;
}
.page-header h1 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
}

.explorer-layout {
  display: flex;
  flex: 1;
  overflow: hidden;
}

/* Tree pane */
.tree-pane {
  width: 280px;
  min-width: 280px;
  border-right: 1px solid var(--rule);
  overflow-y: auto;
  background: var(--bg);
}
.tree-empty {
  padding: 20px;
  text-align: center;
  color: var(--ink-4);
  font-size: 12px;
}
.tree-error {
  color: var(--accent);
}
.tree-list {
  padding: 4px 0;
}
.tree-row {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 3px 12px;
  cursor: pointer;
  font-size: 12px;
  font-family: var(--font-sans);
  color: var(--ink);
  line-height: 1.6;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tree-row:hover {
  background: var(--bg-hover);
}
.tree-row.selected {
  background: var(--bg-active);
}
.tree-icon {
  flex-shrink: 0;
  width: 14px;
  text-align: center;
  font-size: 9px;
  color: var(--ink-3);
}
.file-icon {
  font-size: 10px;
  color: var(--ink-4);
}
.tree-name {
  overflow: hidden;
  text-overflow: ellipsis;
}

/* Content pane */
.content-pane {
  flex: 1;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  background: var(--bg);
}
.content-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--ink-4);
  font-size: 13px;
}
.content-view {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
.content-header {
  display: flex;
  align-items: baseline;
  gap: 10px;
  padding: 8px 16px;
  border-bottom: 1px solid var(--rule);
  background: var(--bg-card);
  flex-shrink: 0;
}
.content-path {
  font-size: 13px;
  font-weight: 500;
  color: var(--ink);
}
.content-fullpath {
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--ink-4);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.content-code {
  flex: 1;
  margin: 0;
  padding: 12px 16px;
  font-family: var(--font-mono);
  font-size: 12px;
  line-height: 1.6;
  color: var(--ink-2);
  background: var(--bg-sunk);
  overflow: auto;
  white-space: pre;
  tab-size: 4;
  border-radius: 0;
}
</style>
