<script setup lang="ts">
import { computed } from 'vue';
import { api } from '../api/client';
import type { Task } from '../api/types';
import { renderMarkdown } from '../lib/markdown';

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

function sandboxLabel(task: Task): string {
  const id = task.sandbox;
  if (!id) return 'Default';
  if (id === 'claude') return 'Claude';
  if (id === 'codex') return 'Codex';
  return id.charAt(0).toUpperCase() + id.slice(1);
}

const promptHtml = computed(() => {
  const t = props.task;
  if (!t.prompt) return '';
  // Show prompt preview only when there's no title shown above (mirrors old UI's
  // cardDisplayPrompt behavior in spirit) and for non-result statuses.
  return renderMarkdown(t.prompt);
});

const resultHtml = computed(() => {
  const t = props.task;
  if (!t.result) return '';
  return renderMarkdown(t.result);
});

const showPromptPreview = computed(() => !!props.task.prompt);

const showResultPreview = computed(() => {
  const t = props.task;
  if (!t.result) return false;
  if (t.status === 'in_progress') return false;
  if (t.status === 'failed') return false;
  if (t.status === 'waiting') return false;
  return true;
});

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
    <!-- Row 1: status badge + meta-right (sandbox, timeout, time) -->
    <div class="flex items-center justify-between mb-1">
      <div class="flex items-center gap-1.5">
        <span :class="['badge', badgeClass(props.task)]">{{ statusLabel(props.task) }}</span>
        <span v-if="showSpinner(props.task)" class="spinner"></span>
        <span
          v-if="props.task.failure_category"
          class="badge badge-failure-category"
          :title="'Failure reason: ' + props.task.failure_category"
          style="font-family:monospace;font-size:9px;"
        >{{ props.task.failure_category }}</span>
      </div>
      <div class="flex items-center gap-1.5 card-meta-right">
        <span
          class="text-xs text-v-muted"
          :title="'Sandbox: ' + sandboxLabel(props.task)"
        >{{ sandboxLabel(props.task) }}</span>
        <span class="text-xs text-v-muted" title="Timeout">{{ formatTimeout(props.task.timeout) }}</span>
        <span class="text-xs text-v-muted" :title="'Created ' + props.task.created_at">{{ timeAgo(props.task.created_at) }}</span>
      </div>
    </div>

    <!-- Row 2: title -->
    <div v-if="props.task.title" class="card-title">{{ props.task.title }}</div>

    <!-- Row 3: tags -->
    <div v-if="props.task.tags?.length" class="tag-chip-row">
      <span
        v-for="tag in props.task.tags"
        :key="tag"
        :class="['tag-chip', { 'badge-routine-spawn': isSpawnedByTag(tag) }]"
        :data-tag="tag"
        :style="isSpawnedByTag(tag) ? '' : tagStyle(tag)"
        :title="tag"
      >{{ tag }}</span>
    </div>

    <!-- Row 4: prompt preview (markdown) -->
    <div
      v-if="showPromptPreview"
      class="text-xs card-prose overflow-hidden"
      style="max-height:4.5em;"
      v-html="promptHtml"
    ></div>

    <!-- Row 5 (failed): error block + stop reason -->
    <template v-if="props.task.status === 'failed' && props.task.result">
      <div class="card-error-reason">
        <span class="card-error-label">Error</span><span class="card-error-text">{{ errorSnippet(props.task) }}</span>
      </div>
      <div v-if="props.task.stop_reason" style="margin-top:4px;">
        <span class="badge badge-failed" style="font-size:9px;">{{ props.task.stop_reason }}</span>
      </div>
    </template>

    <!-- Row 5 (waiting): output block -->
    <div v-else-if="props.task.status === 'waiting' && props.task.result" class="card-output-reason">
      <span class="card-output-label">Output</span><span class="card-output-text">{{ waitingSnippet(props.task) }}</span>
    </div>

    <!-- Row 5 (done/cancelled): result preview (markdown) -->
    <div
      v-else-if="showResultPreview"
      class="text-xs text-v-secondary mt-1 card-prose overflow-hidden"
      style="max-height:3.2em;"
      v-html="resultHtml"
    ></div>

    <!-- Row 7: meta footer (turns, cost, session id) -->
    <div
      v-if="showCostMeta(props.task) && (props.task.status === 'in_progress' || props.task.status === 'waiting' || props.task.status === 'done')"
      class="card-meta"
      style="display:flex;align-items:center;gap:8px;margin-top:6px;font-size:10px;color:var(--ink-4);"
    >
      <span v-if="props.task.turns > 0" class="card-meta-time" :title="'Turns: ' + props.task.turns">{{ props.task.turns }} turn{{ props.task.turns === 1 ? '' : 's' }}</span>
      <span class="card-meta-cost" title="Total cost">{{ formatCost(props.task.usage.cost_usd) }}</span>
      <span
        v-if="props.task.session_id"
        class="card-meta-session"
        style="margin-left:auto;font-family:var(--font-mono);"
        :title="'Session: ' + props.task.session_id"
      >{{ props.task.session_id.slice(0, 7) }}</span>
    </div>

    <!-- Row 8: action buttons -->
    <div
      v-if="!props.task.archived && (props.task.status === 'backlog' || props.task.status === 'waiting' || props.task.status === 'failed' || props.task.status === 'cancelled' || props.task.status === 'done')"
      class="card-actions"
    >
      <button v-if="props.task.status === 'backlog'" class="card-action-btn card-action-start" @click="startTask">&#9654; Start</button>
      <button v-if="props.task.status === 'waiting'" class="card-action-btn card-action-done" @click="doneTask">&#10003; Done</button>
      <button v-if="props.task.status === 'failed' || props.task.status === 'cancelled' || props.task.status === 'done'" class="card-action-btn card-action-retry" @click="retryTask">&#8617; Retry</button>
    </div>
  </div>
</template>
