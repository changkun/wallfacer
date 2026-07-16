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
          <select v-model="selectedPath" class="af-picker" :disabled="artifacts.length < 2" aria-label="Select artifact">
            <option v-for="a in artifacts" :key="a.path" :value="a.path">{{ a.path }}</option>
          </select>
          <svg class="af-picker-caret" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"></polyline></svg>
        </div>
        <span v-if="selected" class="af-meta">{{ formatSize(selected.size) }} · {{ formatDate(selected.modified) }}</span>
      </div>
      <div class="af-bar-right">
        <button class="af-btn" :disabled="loading" @click="load">Refresh</button>
        <a class="af-btn af-btn--ghost" :href="previewUrl" target="_blank" rel="noopener">Direct link</a>
        <button class="af-btn af-btn--primary" :disabled="!selected" @click="openInTab">Open ↗</button>
      </div>
    </div>

    <div class="af-stage">
      <div v-if="loading && !artifacts.length" class="af-state">Loading…</div>
      <div v-else-if="error" class="af-state af-state--err">{{ error }}</div>

      <div v-else-if="!artifacts.length" class="af-empty">
        <div class="af-empty-inner">
          <h2>No artifacts yet</h2>
          <p>Create a self-contained HTML file under <code>artifacts/</code> in your workspace, for example from chat:</p>
          <pre class="af-hint">Create a slide deck about X as a single self-contained
HTML file at artifacts/deck.html</pre>
          <p class="af-muted">Files written by chat and spec agents appear immediately. Task-created files appear after the task's branch is merged.</p>
          <button class="af-btn" style="margin-top: 0.75rem" :disabled="loading" @click="load">Refresh</button>
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
.af {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  background: var(--bg);
}

/* Slim toolbar so the preview gets the whole content area. */
.af-bar {
  flex: none;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  padding: 0.5rem 0.85rem;
  border-bottom: 1px solid var(--rule);
  background: var(--bg-elevated);
}
.af-bar-left {
  display: flex;
  align-items: center;
  gap: 0.75rem;
  min-width: 0;
}
.af-bar-right {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex: none;
}
.af-picker-wrap {
  position: relative;
  display: inline-flex;
  align-items: center;
}
.af-picker {
  appearance: none;
  font: inherit;
  font-size: 0.86rem;
  font-weight: 600;
  color: var(--ink);
  background: var(--bg-input);
  border: 1px solid var(--rule);
  border-radius: var(--radius-md);
  padding: 0.35rem 1.9rem 0.35rem 0.7rem;
  max-width: 46ch;
  text-overflow: ellipsis;
  cursor: pointer;
}
.af-picker:disabled { cursor: default; opacity: 0.9; }
.af-picker-caret {
  position: absolute;
  right: 0.6rem;
  color: var(--ink-3);
  pointer-events: none;
}
.af-meta {
  font-family: var(--font-mono);
  font-size: 0.72rem;
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
  color: var(--ink-3);
}
.af-state--err { color: var(--accent); }

.af-empty {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  padding: 1.5rem;
}
.af-empty-inner {
  max-width: 46ch;
  text-align: center;
}
.af-empty-inner h2 {
  font-size: 1.1rem;
  color: var(--ink);
  margin: 0 0 0.5rem;
}
.af-empty-inner p {
  color: var(--ink-3);
  font-size: 0.92rem;
  line-height: 1.5;
  margin: 0.4rem 0;
}
.af-empty code {
  font-family: var(--font-mono);
  font-size: 0.85em;
  background: var(--bg-sunk);
  border: 1px solid var(--rule);
  border-radius: 4px;
  padding: 0.05em 0.35em;
  color: var(--ink-2);
}
.af-hint {
  text-align: left;
  font-family: var(--font-mono);
  font-size: 0.82rem;
  background: var(--bg-sunk);
  border: 1px solid var(--rule);
  border-radius: var(--radius-md);
  padding: 0.8rem 1rem;
  color: var(--ink-2);
  margin: 0.8rem 0;
  white-space: pre-wrap;
}
.af-muted { color: var(--ink-4); font-size: 0.82rem; }

.af-btn {
  font: inherit;
  font-size: 0.84rem;
  padding: 0.38rem 0.75rem;
  border-radius: var(--radius-md);
  border: 1px solid var(--rule);
  background: var(--bg-elevated);
  color: var(--ink-2);
  cursor: pointer;
  text-decoration: none;
  transition: border-color 0.12s, color 0.12s, background 0.12s;
}
.af-btn:hover { border-color: var(--rule-2); color: var(--ink); }
.af-btn:disabled { opacity: 0.5; cursor: default; }
.af-btn--ghost { background: transparent; }
.af-btn--primary {
  background: var(--accent);
  border-color: var(--accent);
  color: #fff;
}
.af-btn--primary:hover { background: var(--accent-hover); border-color: var(--accent-hover); color: #fff; }
</style>
