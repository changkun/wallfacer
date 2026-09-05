<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { api, withAuthToken } from '../api/client';

interface ArtifactInfo {
  name: string;
  path: string;
  url: string;
  size: number;
  modified: string;
}

const artifacts = ref<ArtifactInfo[]>([]);
const selectedPath = ref<string>('');
const loading = ref(true);
const error = ref('');

const selected = computed(() => artifacts.value.find((a) => a.path === selectedPath.value) ?? null);
const previewUrl = computed(() => (selected.value ? withAuthToken(selected.value.url) : ''));

async function load() {
  loading.value = true;
  error.value = '';
  try {
    const res = await api<{ artifacts: ArtifactInfo[] }>('GET', '/api/artifacts');
    artifacts.value = res.artifacts ?? [];
    // Keep the current selection if it survives a refresh, else pick the newest.
    if (!artifacts.value.some((a) => a.path === selectedPath.value)) {
      selectedPath.value = artifacts.value[0]?.path ?? '';
    }
  } catch (e) {
    error.value = e instanceof Error ? e.message : 'Failed to load artifacts';
  } finally {
    loading.value = false;
  }
}

function openInTab() {
  if (selected.value) window.open(withAuthToken(selected.value.url), '_blank', 'noopener');
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
}

onMounted(load);
</script>

<template>
  <div class="af">
    <div v-if="artifacts.length" class="af-bar">
      <div class="af-bar-left">
        <div class="af-picker-wrap">
          <select v-model="selectedPath" class="field af-picker" :disabled="artifacts.length < 2" aria-label="Select artifact">
            <option v-for="a in artifacts" :key="a.path" :value="a.path">{{ a.path }}</option>
          </select>
          <svg class="af-picker-caret" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><polyline points="6 9 12 15 18 9"></polyline></svg>
        </div>
        <span v-if="selected" class="af-meta">{{ formatSize(selected.size) }} · {{ formatDate(selected.modified) }}</span>
      </div>
      <div class="af-bar-right">
        <button type="button" class="btn sm ghost" :disabled="loading" @click="load">Refresh</button>
        <a class="btn sm ghost" :href="previewUrl" target="_blank" rel="noopener">Direct link</a>
        <button type="button" class="btn sm" :disabled="!selected" @click="openInTab">Open ↗</button>
      </div>
    </div>

    <div class="af-stage">
      <div v-if="loading && !artifacts.length" class="af-state">Loading…</div>
      <div v-else-if="error" class="af-state af-state--err">{{ error }}</div>

      <div v-else-if="!artifacts.length" class="af-empty">
        <div class="af-empty-inner">
          <span class="eyebrow">No artifacts yet</span>
          <p>Create a self-contained HTML file under <code>artifacts/</code> in the workspace, for example from chat:</p>
          <pre class="af-hint">Create a slide deck about X as a single self-contained
HTML file at artifacts/deck.html</pre>
          <p class="af-muted">Files written by chat and spec agents appear immediately. Task-created files appear after the task's branch is merged.</p>
          <button type="button" class="btn sm ghost af-empty-refresh" :disabled="loading" @click="load">Refresh</button>
        </div>
      </div>

      <iframe
        v-else-if="selected"
        :src="previewUrl"
        class="af-frame"
        title="Artifact preview"
        allow="fullscreen"
        sandbox="allow-scripts allow-same-origin allow-popups allow-forms allow-modals"
      ></iframe>
    </div>
  </div>
</template>

<style scoped>
/* The artifact viewer: a 44px tool bar (picker, meta, actions) over a stage
   that the preview iframe fills. The empty state is an eyebrow and a hint. */
.af {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  background: var(--bg);
}
.af-bar {
  flex: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  height: 44px;
  padding: 0 12px;
  border-bottom: 1px solid var(--rule);
  background: var(--bg-sunk);
}
.af-bar-left {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}
.af-bar-right {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: none;
}
.af-picker-wrap {
  position: relative;
  display: inline-flex;
  align-items: center;
}
.af-picker {
  appearance: none;
  min-height: 30px;
  padding: 4px 28px 4px 10px;
  font-family: var(--font-mono);
  font-size: 12px;
  font-weight: 500;
  max-width: 46ch;
  text-overflow: ellipsis;
  cursor: pointer;
  background: var(--bg-card);
}
.af-picker:disabled {
  cursor: default;
  color: var(--ink);
}
.af-picker-caret {
  position: absolute;
  right: 9px;
  color: var(--ink-4);
  pointer-events: none;
}
.af-meta {
  font-family: var(--font-mono);
  font-size: var(--fs-10);
  color: var(--ink-4);
  white-space: nowrap;
}

/* The stage fills everything below the toolbar; the iframe fills the stage. */
.af-stage {
  flex: 1;
  min-height: 0;
  position: relative;
  background: var(--bg-deep);
}
.af-frame {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  border: 0;
  display: block;
}
.af-state {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  font-size: var(--fs-base);
  color: var(--ink-3);
}
.af-state--err {
  color: var(--err);
}
.af-empty {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 24px;
}
.af-empty-inner {
  max-width: 46ch;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  text-align: center;
}
.af-empty-inner p {
  margin: 0;
  font-size: var(--fs-md);
  line-height: 1.5;
  color: var(--ink-2);
}
.af-empty code {
  font-family: var(--font-mono);
  font-size: 0.9em;
  padding: 1px 5px;
  border: 1px solid var(--rule);
  border-radius: var(--r-xs);
  background: var(--bg-sunk);
  color: var(--ink);
}
.af-hint {
  width: 100%;
  margin: 4px 0;
  padding: 12px 14px;
  text-align: left;
  font: 12.5px / 1.5 var(--font-mono);
  border: 1px solid var(--rule);
  border-radius: var(--r-lg);
  background: var(--bg-sunk);
  color: var(--ink-2);
  white-space: pre-wrap;
}
.af-muted {
  color: var(--ink-3);
  font-size: var(--fs-base);
}
.af-empty-refresh {
  margin-top: 6px;
}
</style>
