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

function previewLines(): string[] {
  if (fileContent.value == null) return [];
  return fileContent.value.split('\n');
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
  <div class="board-with-explorer explorer-page-root">
    <aside class="explorer-panel">
      <div class="explorer-panel__header">
        <span class="explorer-panel__title">Explorer</span>
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
            @click="entry.is_dir ? toggleDir(entry) : selectFile(entry)"
          >
            <span class="explorer-node__toggle">
              <template v-if="entry.is_dir">{{ expanded.has(entry.path) ? '▼' : '▶' }}</template>
            </span>
            <span class="explorer-node__icon">{{ entry.is_dir ? '▣' : '·' }}</span>
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
        </div>
        <div class="explorer-preview__content">
          <pre class="explorer-preview__code"><code>
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
</style>
