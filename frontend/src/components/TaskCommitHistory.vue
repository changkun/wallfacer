<script setup lang="ts">
import { ref, watch, onBeforeUnmount } from 'vue';
import { api } from '../api/client';
import type { TaskCommit } from '../api/types';

const props = defineProps<{ taskId: string; active: boolean; refreshKey: number }>();
const commits = ref<TaskCommit[]>([]);
const error = ref('');
const loading = ref(false);
let generation = 0;
watch(() => props.taskId, () => { commits.value = []; error.value = ''; });
watch(() => [props.taskId, props.active, props.refreshKey], async () => {
  const request = ++generation;
  if (!props.active) { loading.value = false; return; }
  loading.value = true;
  error.value = '';
  try {
    const result = await api<TaskCommit[]>('GET', `/api/tasks/${props.taskId}/commits`);
    if (request === generation) commits.value = result;
  } catch (e) {
    if (request === generation) error.value = e instanceof Error ? e.message : String(e);
  } finally {
    if (request === generation) loading.value = false;
  }
}, { immediate: true });
onBeforeUnmount(() => { generation++; });
</script>

<template>
  <section class="task-commits" aria-label="Task commit history">
    <h3>Task commits <span v-if="commits.length">({{ commits.length }})</span></h3>
    <p v-if="error" role="alert">Could not load commit history: {{ error }}</p>
    <p v-else-if="loading && !commits.length" role="status">Loading commits…</p>
    <p v-else-if="!commits.length">No recorded commits. Uncommitted edits appear in the aggregate diff below.</p>
    <details v-for="commit in commits" :key="`${commit.repository}:${commit.hash}`" class="task-commit">
      <summary>
        <span class="commit-subject"><code>{{ commit.hash.slice(0, 8) }}</code> {{ commit.subject }}</span>
        <span class="commit-meta">{{ commit.repository }} · Attempt {{ commit.attempt }}, turn {{ commit.turn }} · {{ commit.author }} · {{ new Date(commit.authored_at).toLocaleString() }}</span>
      </summary>
      <p v-if="commit.patch_truncated" role="note">Patch preview limited to 2 MiB.</p>
      <pre v-if="commit.patch">{{ commit.patch }}</pre>
      <p v-else>This commit contains no text changes.</p>
    </details>
  </section>
</template>

<style scoped>
.task-commits { margin-bottom: 18px; min-width: 0; }
.task-commits h3 { font-size: 13px; margin: 0 0 8px; }
.task-commits p, .commit-meta { color: var(--text-muted); font-size: 12px; }
.task-commit { border: 1px solid var(--border); border-radius: 6px; margin: 6px 0; overflow: hidden; }
.task-commit summary { cursor: pointer; padding: 10px; }
.commit-subject { font-size: 13px; overflow-wrap: anywhere; }
.commit-subject code { color: var(--text-muted); margin-right: 6px; }
.commit-meta { display: block; margin-top: 4px; overflow-wrap: anywhere; }
.task-commit pre { margin: 0; padding: 12px; border-top: 1px solid var(--border); max-height: 360px; overflow: auto; font-size: 12px; tab-size: 4; }
.task-commit > p { padding: 0 12px; }
</style>
