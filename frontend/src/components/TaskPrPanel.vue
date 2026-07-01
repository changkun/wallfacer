<script setup lang="ts">
// PR section for a task (task-centric GitHub redesign): shows the task branch's
// pull request (create / view / status / comment), derived server-side from the
// task's workspace repo + branch. Self-contained so TaskDetail hosts it with a
// single line.
import { computed, onMounted, ref, watch } from 'vue';
import type { Task } from '../api/types';
import { useGithubPrStore } from '../stores/githubPr';

const props = defineProps<{ task: Task }>();
const pr = useGithubPrStore();

const hasBranch = computed(() => !!props.task.branch_name);
const current = computed(() => pr.prFor(props.task.id));
const busy = computed(() => !!pr.loading[props.task.id]);
const draft = ref('');

async function load() {
  if (hasBranch.value) await pr.fetchTaskPR(props.task.id);
}
onMounted(load);
watch(() => props.task.id, load);

async function create() {
  await pr.createTaskPR(props.task.id);
}
async function postComment() {
  if (!draft.value.trim()) return;
  const ok = await pr.commentTaskPR(props.task.id, draft.value);
  if (ok) draft.value = '';
}
</script>

<template>
  <div v-if="hasBranch" class="pr-panel">
    <div class="mdl-h">Pull Request</div>

    <div v-if="busy && current === undefined" class="pr-muted">Checking…</div>

    <template v-else-if="current">
      <div class="pr-row">
        <a class="pr-link" :href="current.html_url" target="_blank" rel="noopener">
          #{{ current.number }} {{ current.title }}
        </a>
        <span class="pr-state" :class="`pr-state--${current.state}`">{{ current.state }}</span>
      </div>
      <textarea v-model="draft" class="pr-comment" rows="2" placeholder="Comment on the PR…"></textarea>
      <button class="pr-btn" :disabled="!draft.trim()" @click="postComment">Comment</button>
    </template>

    <template v-else>
      <p class="pr-muted">No pull request yet.</p>
      <button class="pr-btn pr-btn--primary" :disabled="busy" @click="create">
        {{ busy ? 'Creating…' : 'Create PR' }}
      </button>
    </template>

    <p v-if="pr.error" class="pr-err">{{ pr.error }}</p>
  </div>
</template>

<style scoped>
.pr-panel { display: flex; flex-direction: column; gap: 0.4rem; }
.pr-row { display: flex; align-items: center; gap: 0.5rem; flex-wrap: wrap; }
.pr-link { color: var(--accent, #3b82f6); text-decoration: none; overflow: hidden; text-overflow: ellipsis; }
.pr-state { font-size: 0.7rem; text-transform: uppercase; padding: 1px 6px; border-radius: 4px; border: 1px solid var(--border, #444); }
.pr-state--open { color: #22c55e; border-color: #22c55e; }
.pr-state--closed { color: #ef4444; border-color: #ef4444; }
.pr-state--merged { color: #a855f7; border-color: #a855f7; }
.pr-muted { color: var(--text-muted, #888); margin: 0; }
.pr-comment { width: 100%; padding: 0.4rem; border: 1px solid var(--border, #444); border-radius: 6px; background: var(--surface, #161616); color: inherit; resize: vertical; font: inherit; }
.pr-btn { align-self: flex-start; padding: 0.3rem 0.8rem; border: 1px solid var(--border, #444); border-radius: 6px; background: transparent; color: inherit; cursor: pointer; }
.pr-btn--primary { background: var(--accent, #2563eb); color: #fff; border-color: transparent; }
.pr-btn:disabled { opacity: 0.5; cursor: not-allowed; }
.pr-err { color: var(--danger, #dc2626); font-size: 0.8rem; margin: 0; }
</style>
