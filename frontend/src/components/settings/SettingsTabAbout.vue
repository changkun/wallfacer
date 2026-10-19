<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api } from '../../api/client';

interface TaskStates {
  in_progress?: number;
  waiting?: number;
  backlog?: number;
  done?: number;
  failed?: number;
}

interface RuntimeStatus {
  go_goroutine_count?: number;
  go_heap_alloc_bytes?: number;
  active_containers?: number;
  task_states?: TaskStates;
}

const status = ref<RuntimeStatus | null>(null);

function formatBytes(bytes: number): string {
  if (bytes < 1024) return bytes + ' B';
  const KB = 1024,
    MB = KB * 1024,
    GB = MB * 1024;
  if (bytes < MB) return (bytes / KB).toFixed(2) + ' KB';
  if (bytes < GB) return (bytes / MB).toFixed(2) + ' MB';
  return (bytes / GB).toFixed(2) + ' GB';
}

function taskStatesText(ts: TaskStates): string {
  const parts: string[] = [];
  if (ts.in_progress) parts.push(ts.in_progress + ' running');
  if (ts.waiting) parts.push(ts.waiting + ' waiting');
  if (ts.backlog) parts.push(ts.backlog + ' backlog');
  if (ts.done) parts.push(ts.done + ' done');
  if (ts.failed) parts.push(ts.failed + ' failed');
  return parts.join(' · ');
}

function hasTaskStates(ts: TaskStates | undefined): boolean {
  if (!ts) return false;
  return !!(ts.in_progress || ts.waiting || ts.backlog || ts.done || ts.failed);
}

onMounted(async () => {
  try {
    const data = await api<RuntimeStatus>('GET', '/api/debug/runtime');
    status.value = data;
  } catch {
    status.value = null;
  }
});
</script>

<template>
  <div class="card compact" data-settings-tab="about">
    <div class="card-head"><span class="eyebrow">About</span></div>
    <div class="about-brand">
      <span class="about-mark" aria-hidden="true">
        <svg width="20" height="20" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" style="display:block;image-rendering:pixelated">
          <rect x="0" y="0" width="6" height="3" fill="var(--accent)" /><rect x="7" y="0" width="9" height="3" fill="var(--accent-2)" />
          <rect x="0" y="4" width="4" height="3" fill="var(--accent-2)" /><rect x="5" y="4" width="6" height="3" fill="var(--accent)" /><rect x="12" y="4" width="4" height="3" fill="var(--accent-2)" />
          <rect x="0" y="8" width="7" height="3" fill="var(--accent-2)" /><rect x="8" y="8" width="8" height="3" fill="var(--accent-2)" />
          <rect x="0" y="12" width="3" height="4" fill="var(--accent)" /><rect x="4" y="12" width="6" height="4" fill="var(--accent-2)" /><rect x="11" y="12" width="5" height="4" fill="var(--accent)" />
        </svg>
      </span>
      <div class="about-brand__text">
        <a href="https://github.com/changkun/wallfacer" target="_blank" rel="noopener noreferrer" class="about-brand__name wallfacer-brand">Wallfacer</a>
        <span class="set-row__help">Dispatch AI agents. Collect merged code.</span>
      </div>
    </div>
    <div class="rows">
      <div class="set-row">
        <div class="set-row__main"><span class="set-row__label">Source</span></div>
        <div class="set-row__end"><a class="link mono" href="https://github.com/changkun/wallfacer" target="_blank" rel="noopener noreferrer">github.com/changkun/wallfacer</a></div>
      </div>
      <div class="set-row">
        <div class="set-row__main"><span class="set-row__label">License</span></div>
        <div class="set-row__end"><span class="set-row__help">MIT · Copyright © 2026 <a href="https://changkun.de" target="_blank" rel="noopener noreferrer" class="link">Changkun Ou</a></span></div>
      </div>
    </div>
  </div>

  <div v-if="status" class="card compact">
    <div class="card-head"><span class="eyebrow">System status</span></div>
    <div class="rows">
      <div class="set-row">
        <div class="set-row__main"><span class="set-row__label">Runtime</span></div>
        <div class="set-row__end"><span class="mono set-row__help">{{ status.go_goroutine_count || 0 }} goroutines · {{ formatBytes(status.go_heap_alloc_bytes || 0) }} heap</span></div>
      </div>
      <div class="set-row">
        <div class="set-row__main"><span class="set-row__label">Active agents</span></div>
        <div class="set-row__end"><span class="mono">{{ status.active_containers || 0 }}</span></div>
      </div>
      <div v-if="hasTaskStates(status.task_states)" class="set-row">
        <div class="set-row__main"><span class="set-row__label">Tasks</span></div>
        <div class="set-row__end"><span class="mono set-row__help">{{ taskStatesText(status.task_states!) }}</span></div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.about-brand {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 14px 14px 6px;
}
.about-mark {
  width: 34px;
  height: 34px;
  border-radius: var(--r-sm);
  background: var(--bg-sunk);
  display: grid;
  place-items: center;
  flex: none;
}
.about-brand__text {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.about-brand__name {
  font-size: var(--fs-xl);
  text-decoration: none;
  line-height: 1.2;
}
</style>
