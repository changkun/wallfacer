<script setup lang="ts">
import { api } from '../api/client';
import type { Task } from '../api/types';

const props = defineProps<{ task: Task }>();

function statusLabel(task: Task): string {
  if (task.archived) return 'archived';
  if (task.status === 'in_progress') return 'in progress';
  if (task.status === 'committing') return 'committing';
  return task.status;
}

function badgeClass(task: Task): string {
  if (task.archived) return 'badge-archived';
  return `badge-${task.status}`;
}

function cardClasses(task: Task): Record<string, boolean> {
  return {
    card: true,
    [`card-${task.status}`]: true,
    'card-failed-waiting': task.status === 'failed',
    'card-cancelled-done': task.status === 'cancelled',
  };
}

function formatTimeout(minutes: number): string {
  if (!minutes) return '5m';
  if (minutes < 60) return minutes + 'm';
  if (minutes % 60 === 0) return (minutes / 60) + 'h';
  return Math.floor(minutes / 60) + 'h' + (minutes % 60) + 'm';
}

function timeAgo(iso: string): string {
  const sec = Math.floor((Date.now() - new Date(iso).getTime()) / 1000);
  if (sec < 60) return 'just now';
  if (sec < 3600) return Math.floor(sec / 60) + 'm ago';
  if (sec < 86400) return Math.floor(sec / 3600) + 'h ago';
  return Math.floor(sec / 86400) + 'd ago';
}

function snippet(text: string, n = 160): string {
  if (!text) return '';
  return text.length > n ? text.slice(0, n) + '…' : text;
}

function errorSnippet(task: Task): string {
  if (task.status !== 'failed' || !task.result) return '';
  return snippet(task.result);
}

function waitingSnippet(task: Task): string {
  if (task.status !== 'waiting' || !task.result) return '';
  return snippet(task.result);
}

function tagStyle(tag: string): string {
  let sum = 0;
  for (let i = 0; i < tag.length; i++) sum += tag.charCodeAt(i);
  const n = sum % 12;
  return `background:var(--tag-bg-${n});color:var(--tag-text-${n});`;
}

function isSpawnedByTag(tag: string): boolean {
  return tag.toLowerCase().startsWith('spawned-by:');
}

function promptPreview(task: Task): string {
  if (!task.prompt) return '';
  return task.prompt.length > 200 ? task.prompt.slice(0, 200) + '…' : task.prompt;
}

function showSpinner(task: Task): boolean {
  return task.status === 'in_progress' || task.status === 'committing';
}

function formatCost(usd: number): string {
  if (!usd || usd <= 0) return '';
  if (usd < 0.01) return '<$0.01';
  return '$' + usd.toFixed(2);
}

function showCostMeta(task: Task): boolean {
  return !!(task.usage && task.usage.cost_usd > 0);
}

async function startTask(e: Event) {
  e.stopPropagation();
  await api('PATCH', `/api/tasks/${props.task.id}`, { status: 'in_progress' });
}

async function retryTask(e: Event) {
  e.stopPropagation();
  await api('PATCH', `/api/tasks/${props.task.id}`, { status: 'backlog' });
}

async function doneTask(e: Event) {
  e.stopPropagation();
  await api('POST', `/api/tasks/${props.task.id}/done`);
}
</script>

<template>
  <div :class="cardClasses(props.task)">
    <!-- Title -->
    <div v-if="props.task.title" class="card-title">{{ props.task.title }}</div>

    <!-- Status badge row -->
    <div style="display:flex;align-items:center;gap:4px;margin-bottom:4px;">
      <span :class="['badge', badgeClass(props.task)]">{{ statusLabel(props.task) }}</span>
      <span v-if="showSpinner(props.task)" class="spinner"></span>
      <div class="card-meta-right" style="display:flex;align-items:center;gap:6px;margin-left:auto;">
        <span v-if="props.task.sandbox" :title="'Sandbox: ' + props.task.sandbox">{{ props.task.sandbox }}</span>
        <span v-if="props.task.timeout" title="Timeout">{{ formatTimeout(props.task.timeout) }}</span>
        <span :title="'Created ' + props.task.created_at">{{ timeAgo(props.task.created_at) }}</span>
      </div>
    </div>

    <!-- Prompt preview (backlog) -->
    <div v-if="props.task.status === 'backlog' && !props.task.title && props.task.prompt" class="card-prose" style="max-height:4.5em;overflow:hidden;font-size:12px;">
      {{ promptPreview(props.task) }}
    </div>

    <!-- Tags -->
    <div v-if="props.task.tags?.length" class="tag-chip-row">
      <span
        v-for="tag in props.task.tags"
        :key="tag"
        :class="['tag-chip', { 'badge-routine-spawn': isSpawnedByTag(tag) }]"
        :style="isSpawnedByTag(tag) ? '' : tagStyle(tag)"
        :title="'Tag: ' + tag"
      >{{ tag }}</span>
    </div>

    <!-- Error (failed) -->
    <div v-if="props.task.status === 'failed' && errorSnippet(props.task)" class="card-error-reason">
      <span class="card-error-label">Error</span><span class="card-error-text">{{ errorSnippet(props.task) }}</span>
    </div>

    <!-- Stop reason chip (failed) -->
    <div v-if="props.task.status === 'failed' && props.task.stop_reason" style="margin-top:4px;">
      <span class="badge badge-failed" style="font-size:9px;">{{ props.task.stop_reason }}</span>
    </div>

    <!-- Output (waiting) -->
    <div v-if="props.task.status === 'waiting' && waitingSnippet(props.task)" class="card-output-reason">
      <span class="card-output-label">Output</span><span class="card-output-text">{{ waitingSnippet(props.task) }}</span>
    </div>

    <!-- Cost / activity meta (in_progress, waiting, done) -->
    <div
      v-if="showCostMeta(props.task) && (props.task.status === 'in_progress' || props.task.status === 'waiting' || props.task.status === 'done')"
      class="card-meta"
      style="display:flex;align-items:center;gap:8px;margin-top:6px;font-size:10px;color:var(--ink-4);"
    >
      <span v-if="props.task.turns > 0" class="card-meta-time" :title="'Turns: ' + props.task.turns">{{ props.task.turns }} turn{{ props.task.turns === 1 ? '' : 's' }}</span>
      <span class="card-meta-cost" :title="'Total cost'">{{ formatCost(props.task.usage.cost_usd) }}</span>
      <span v-if="props.task.session_id" class="card-meta-session" style="margin-left:auto;font-family:var(--font-mono);" :title="'Session: ' + props.task.session_id">{{ props.task.session_id.slice(0, 7) }}</span>
    </div>

    <!-- Actions -->
    <div v-if="!props.task.archived && (props.task.status === 'backlog' || props.task.status === 'waiting' || props.task.status === 'failed' || props.task.status === 'cancelled' || props.task.status === 'done')" class="card-actions">
      <button v-if="props.task.status === 'backlog'" class="card-action-btn card-action-start" @click="startTask">&#9654; Start</button>
      <button v-if="props.task.status === 'waiting'" class="card-action-btn card-action-done" @click="doneTask">&#10003; Done</button>
      <button v-if="props.task.status === 'failed' || props.task.status === 'cancelled' || props.task.status === 'done'" class="card-action-btn card-action-retry" @click="retryTask">&#8617; Retry</button>
    </div>
  </div>
</template>
