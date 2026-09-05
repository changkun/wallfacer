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

// Offer "Create PR" only while a PR still makes sense. A done task's changes are
// already merged, so creating a PR there is noise -- but an existing PR (e.g.
// the one that merged it) is still worth showing.
const canCreate = computed(() => hasBranch.value && props.task.status !== 'done');

async function load() {
  if (hasBranch.value) await pr.fetchTaskPR(props.task.id);
}
onMounted(load);
watch(() => props.task.id, load);

// The PR state on the ramp: open is live work, merged is published, closed ended.
function stateClass(state: string): string {
  return state === 'open' ? 'pill-ok' : state === 'merged' ? 'pill-pub' : 'pill-err';
}

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
  <div v-if="hasBranch && (current || canCreate)" class="card compact pr-panel">
    <div class="card-head">
      <span class="eyebrow">Pull request</span>
      <span v-if="current" class="pill pr-state" :class="stateClass(current.state)">{{ current.state }}</span>
    </div>
    <div class="pr-body">
      <div v-if="busy && current === undefined" class="muted">Checking…</div>

      <template v-else-if="current">
        <a class="link pr-link" :href="current.html_url" target="_blank" rel="noopener">
          #{{ current.number }} {{ current.title }}
        </a>
        <textarea v-model="draft" class="field pr-comment" rows="2" placeholder="Comment on the PR…"></textarea>
        <button class="btn sm ghost" :disabled="!draft.trim()" @click="postComment">Comment</button>
      </template>

      <template v-else-if="canCreate">
        <p class="muted pr-muted">No pull request yet.</p>
        <button class="btn sm" :disabled="busy" @click="create">
          {{ busy ? 'Creating…' : 'Create PR' }}
        </button>
      </template>

      <p v-if="pr.error" class="pr-err">{{ pr.error }}</p>
    </div>
  </div>
</template>

<style scoped>
.pr-state { margin-left: auto; }
.pr-body { display: flex; flex-direction: column; align-items: flex-start; gap: 8px; padding: 12px 14px; }
.pr-link { max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.pr-comment { resize: vertical; }
.pr-muted { margin: 0; }
.pr-err { color: var(--err); font-size: var(--fs-10); margin: 0; }
</style>
