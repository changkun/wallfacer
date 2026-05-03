<script setup lang="ts">
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
  switch (task.status) {
    case 'in_progress':
    case 'committing': return 'status-progress';
    case 'waiting': return 'status-waiting';
    case 'failed': return 'status-failed';
    case 'done': return 'status-done';
    case 'cancelled': return 'status-cancelled';
    default: return 'status-backlog';
  }
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
</script>

<template>
  <div class="card" :class="statusClass(props.task)">
    <div class="card-header">
      <span class="card-title">{{ props.task.title || props.task.prompt.slice(0, 60) }}</span>
    </div>
    <div class="card-meta">
      <span class="card-id">{{ shortId(props.task.id) }}</span>
      <span class="card-status">{{ statusLabel(props.task) }}</span>
      <span v-if="props.task.usage.cost_usd" class="card-cost">{{ costDisplay(props.task.usage.cost_usd) }}</span>
      <span class="card-time">{{ timeAgo(props.task.updated_at) }}</span>
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

.card-header {
  margin-bottom: 4px;
}
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

.card-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 10px;
  color: var(--ink-4);
  font-family: var(--font-mono);
}
.card-id {
  opacity: 0.7;
}
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

.card-cost {
  color: var(--ink-3);
}
.card-time {
  margin-left: auto;
}
</style>
