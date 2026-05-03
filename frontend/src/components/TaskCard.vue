<script setup lang="ts">
import { api } from '../api/client';
import type { Task } from '../api/types';

const props = defineProps<{ task: Task }>();

function shortId(id: string): string {
  return id.slice(0, 8);
}

function statusLabel(task: Task): string {
  if (task.status === 'in_progress') return 'running';
  if (task.status === 'committing') return 'committing';
  return task.status;
}

function statusClass(task: Task): string {
  const map: Record<string, string> = {
    in_progress: 'status-progress', committing: 'status-progress',
    waiting: 'status-waiting', failed: 'status-failed',
    done: 'status-done', cancelled: 'status-cancelled',
  };
  return map[task.status] || 'status-backlog';
}

function costDisplay(usd: number): string {
  if (usd === 0) return '';
  if (usd < 0.01) return '<$0.01';
  return '$' + usd.toFixed(2);
}

function timeAgo(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime();
  const sec = Math.floor(ms / 1000);
  if (sec < 60) return 'just now';
  const min = Math.floor(sec / 60);
  if (min < 60) return min + 'm ago';
  const hr = Math.floor(min / 60);
  if (hr < 24) return hr + 'h ago';
  const d = Math.floor(hr / 24);
  return d + 'd ago';
}

function errorSnippet(task: Task): string {
  if (task.status !== 'failed' || !task.result) return '';
  return task.result.slice(0, 200);
}

function formatTimeout(seconds: number): string {
  if (seconds >= 3600) return Math.floor(seconds / 3600) + 'h';
  if (seconds >= 60) return Math.floor(seconds / 60) + 'm';
  return seconds + 's';
}

async function retryTask(e: Event) {
  e.stopPropagation();
  await api('PATCH', `/api/tasks/${props.task.id}`, { status: 'backlog' });
}
</script>

<template>
  <div class="card" :class="statusClass(props.task)">
    <div class="card-header">
      <span class="card-title">{{ props.task.title || props.task.prompt.slice(0, 80) }}</span>
    </div>

    <div v-if="props.task.status === 'failed' && errorSnippet(props.task)" class="card-error">
      {{ errorSnippet(props.task) }}
    </div>

    <div v-if="props.task.tags?.length" class="card-tags">
      <span v-for="tag in props.task.tags.slice(0, 3)" :key="tag" class="card-tag">{{ tag }}</span>
    </div>

    <div class="card-meta">
      <span class="card-id">{{ shortId(props.task.id) }}</span>
      <span class="card-status">{{ statusLabel(props.task) }}</span>
      <span v-if="props.task.sandbox" class="card-sandbox">{{ props.task.sandbox }}</span>
      <span v-if="props.task.timeout" class="card-timeout">{{ formatTimeout(props.task.timeout) }}</span>
      <span v-if="props.task.usage.cost_usd" class="card-cost">{{ costDisplay(props.task.usage.cost_usd) }}</span>
      <span class="card-time">{{ timeAgo(props.task.updated_at) }}</span>
    </div>

    <div v-if="props.task.status === 'failed' || props.task.status === 'cancelled'" class="card-actions">
      <button class="card-action-btn" @click="retryTask">Retry</button>
    </div>
  </div>
</template>

<style scoped>
.card {
  padding: 8px 10px;
  margin-bottom: 4px;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  cursor: pointer;
  transition: box-shadow 0.1s, border-color 0.1s;
}
.card:hover {
  box-shadow: var(--sh-2);
  border-color: var(--ink-4);
}

.card-header { margin-bottom: 4px; }
.card-title {
  font-size: 12px;
  font-weight: 500;
  color: var(--ink);
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  line-height: 1.4;
}

.card-error {
  margin: 4px 0;
  padding: 4px 6px;
  background: rgba(163, 45, 45, 0.08);
  border-radius: 2px;
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--err);
  line-height: 1.4;
  overflow: hidden;
  display: -webkit-box;
  -webkit-line-clamp: 4;
  -webkit-box-orient: vertical;
}

.card-tags {
  display: flex;
  gap: 3px;
  margin: 4px 0;
  flex-wrap: wrap;
}
.card-tag {
  padding: 1px 5px;
  background: var(--accent-tint);
  color: var(--accent);
  border-radius: 2px;
  font-size: 9px;
  font-weight: 500;
}

.card-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 10px;
  color: var(--ink-4);
  font-family: var(--font-mono);
}
.card-id { opacity: 0.7; }
.card-status {
  padding: 1px 4px;
  border-radius: 2px;
  font-size: 9px;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.status-progress .card-status { color: var(--col-progress); background: rgba(58, 109, 179, 0.1); }
.status-waiting .card-status { color: var(--col-waiting); background: rgba(165, 106, 18, 0.1); }
.status-failed .card-status { color: var(--err); background: rgba(163, 45, 45, 0.1); }
.status-done .card-status { color: var(--col-done); background: rgba(63, 122, 74, 0.1); }
.status-cancelled .card-status { color: var(--ink-3); background: var(--bg-hover); }
.status-backlog .card-status { color: var(--col-backlog); }
.card-sandbox { color: var(--ink-3); font-size: 9px; }
.card-timeout { color: var(--ink-3); font-size: 9px; }
.card-cost { color: var(--ink-3); }
.card-time { margin-left: auto; }

.card-actions {
  margin-top: 6px;
  display: flex;
  gap: 4px;
}
.card-action-btn {
  padding: 2px 8px;
  background: var(--bg-hover);
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  font-size: 10px;
  color: var(--ink-2);
  cursor: pointer;
}
.card-action-btn:hover { background: var(--bg-active); color: var(--ink); }
</style>
