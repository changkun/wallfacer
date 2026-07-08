<script setup lang="ts">
import { computed, ref } from 'vue';
import type { Task, ReviewTranscript } from '../api/types';
import { renderMarkdown } from '../lib/markdown';

const props = defineProps<{ task: Task; transcript: ReviewTranscript | null }>();

const running = computed(() => props.transcript?.running ?? false);
const config = computed(() => props.transcript?.config ?? null);
const outcome = computed(() => props.transcript?.outcome ?? null);
const forks = computed(() => props.transcript?.forks ?? []);

// A run exists if there is a live trajectory or a persisted verdict on the task.
const hasRun = computed(() => forks.value.length > 0 || props.task.review_unresolved !== undefined);
const unresolved = computed(() => props.task.review_unresolved);

const status = computed<{ label: string; kind: 'running' | 'clean' | 'issues' | 'idle' }>(() => {
  if (running.value) return { label: 'Running', kind: 'running' };
  if (unresolved.value === undefined) return { label: 'Not run', kind: 'idle' };
  if (unresolved.value === 0) return { label: 'Clean', kind: 'clean' };
  return { label: `${unresolved.value} unresolved`, kind: 'issues' };
});

const reviewCost = computed(() => props.task.usage_breakdown?.review?.cost_usd ?? 0);

function fmtDuration(secs: number): string {
  if (!secs) return '';
  if (secs < 60) return `${secs}s`;
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  return s ? `${m}m ${s}s` : `${m}m`;
}

function fmtTokens(n: number): string {
  if (!n) return '';
  return n >= 1000 ? `${(n / 1000).toFixed(1)}k tokens` : `${n} tokens`;
}

const byStatusText = computed(() => {
  const bs = outcome.value?.by_status;
  if (!bs) return '';
  return Object.entries(bs)
    .filter(([, v]) => v > 0)
    .map(([k, v]) => `${v} ${k}`)
    .join(' · ');
});

// Per-fork collapse state; forks default open.
const collapsed = ref<Record<number, boolean>>({});
function toggleFork(i: number) {
  collapsed.value[i] = !collapsed.value[i];
}
</script>

<template>
  <section class="review">
    <header class="review__header">
      <div class="review__title">
        <span class="review__icon" aria-hidden="true">&#9878;</span>
        <span>Adversarial Verification</span>
      </div>
      <span class="review__status" :class="`review__status--${status.kind}`">
        <span v-if="status.kind === 'running'" class="review__dot" aria-hidden="true">&#9679;</span>
        {{ status.label }}
      </span>
    </header>

    <p v-if="config" class="review__config">
      {{ config.forks }} critic fork{{ config.forks === 1 ? '' : 's' }} · up to {{ config.max_rounds }} rounds each ·
      proposer <strong>{{ config.proposer_model }}</strong> ·
      critics <strong>{{ config.critic_models.join(', ') }}</strong> ·
      budget {{ Math.round(config.cost_cap / 1000) }}k tokens
    </p>

    <!-- Outcome (after a completed run) -->
    <div v-if="!running && hasRun" class="review__outcome" :class="`review__outcome--${status.kind}`">
      <div class="review__verdict">
        <template v-if="status.kind === 'clean'">No unresolved attacks — the changes survived the debate.</template>
        <template v-else-if="status.kind === 'issues'">{{ unresolved }} unresolved attack{{ unresolved === 1 ? '' : 's' }} remain.</template>
        <template v-else>Verification complete.</template>
      </div>
      <!-- eslint-disable-next-line vue/no-v-html — renderMarkdown sanitises -->
      <div
        v-if="task.review_headline && (unresolved ?? 0) > 0"
        class="review__headline prose-content review-md"
        v-html="renderMarkdown(task.review_headline)"
      />
      <div class="review__meta">
        <span v-if="byStatusText">{{ byStatusText }}</span>
        <span v-if="outcome?.termination">ended: {{ outcome.termination.replace(/_/g, ' ') }}</span>
        <span v-if="outcome && outcome.wall_seconds">{{ fmtDuration(outcome.wall_seconds) }}</span>
        <span v-if="outcome && outcome.tokens">{{ fmtTokens(outcome.tokens) }}</span>
        <span v-if="reviewCost > 0">${{ reviewCost.toFixed(2) }}</span>
      </div>
    </div>

    <!-- Trajectory: each fork is a debate thread -->
    <div v-if="forks.length" class="review__forks">
      <section v-for="fork in forks" :key="`fork-${fork.index}`" class="review-fork">
        <button type="button" class="review-fork__head" @click="toggleFork(fork.index)">
          <span class="review-fork__chevron" :class="{ 'is-collapsed': collapsed[fork.index] }" aria-hidden="true">&#9662;</span>
          <span class="review-fork__name">Fork {{ fork.index }}</span>
          <span class="review-fork__count">{{ fork.rounds.length }} round{{ fork.rounds.length === 1 ? '' : 's' }}</span>
        </button>
        <div v-show="!collapsed[fork.index]" class="review-fork__thread">
          <article
            v-for="r in fork.rounds"
            :key="`r-${fork.index}-${r.round}-${r.role}`"
            class="review-msg"
            :class="`review-msg--${r.role}`"
          >
            <header class="review-msg__head">
              <span class="review-msg__role">{{ r.role === 'critic' ? 'Critic' : 'Proposer' }}</span>
              <span class="review-msg__round">Round {{ r.round }}</span>
            </header>
            <!-- eslint-disable-next-line vue/no-v-html — renderMarkdown sanitises -->
            <div class="review-msg__body prose-content review-md" v-html="renderMarkdown(r.body)" />
          </article>
        </div>
      </section>
    </div>

    <div v-else-if="running" class="review__empty">Verification running… waiting for the first round to land.</div>
    <div v-else-if="!hasRun" class="review__empty">
      No adversarial verification has run for this task yet. Trigger <strong>Review</strong> from the actions panel.
    </div>
  </section>
</template>

<style scoped>
.review {
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-elevated);
  padding: 0.85rem;
  margin-bottom: 1.25rem;
}
.review__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}
.review__title {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  font-weight: 600;
  font-size: 0.9rem;
}
.review__icon { color: var(--accent); font-size: 1.05rem; }
.review__status {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  font-size: 0.72rem;
  font-weight: 600;
  padding: 0.15rem 0.5rem;
  border-radius: 999px;
  white-space: nowrap;
}
.review__status--running { color: var(--accent); background: color-mix(in srgb, var(--accent) 14%, transparent); }
.review__status--clean { color: var(--ok); background: color-mix(in srgb, var(--ok) 16%, transparent); }
.review__status--issues { color: var(--warn); background: color-mix(in srgb, var(--warn) 18%, transparent); }
.review__status--idle { color: var(--text-muted); background: var(--bg-hover); }
.review__dot { font-size: 0.6rem; animation: review-pulse 1.4s ease-in-out infinite; }
@keyframes review-pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.3; } }

.review__config {
  margin: 0.55rem 0 0;
  font-size: 0.74rem;
  color: var(--text-secondary);
  line-height: 1.5;
}
.review__config strong { color: var(--text); font-weight: 600; }

.review__outcome {
  margin-top: 0.75rem;
  padding: 0.6rem 0.7rem;
  border-radius: 8px;
  border-left: 3px solid var(--border);
  background: var(--bg-sunk);
}
.review__outcome--clean { border-left-color: var(--ok); }
.review__outcome--issues { border-left-color: var(--warn); }
.review__verdict { font-size: 0.82rem; font-weight: 600; }
.review__headline { margin-top: 0.35rem; font-size: 0.8rem; }
.review__meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem 0.75rem;
  margin-top: 0.45rem;
  font-size: 0.72rem;
  color: var(--text-muted);
}

.review__forks { margin-top: 0.85rem; display: flex; flex-direction: column; gap: 0.6rem; }
.review-fork {
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
  background: var(--bg-card);
}
.review-fork__head {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  width: 100%;
  padding: 0.45rem 0.6rem;
  background: var(--bg-hover);
  border: 0;
  cursor: pointer;
  font: inherit;
  color: var(--text);
  text-align: left;
}
.review-fork__chevron {
  font-size: 0.7rem;
  color: var(--text-muted);
  transition: transform 0.15s ease;
  line-height: 1;
}
.review-fork__chevron.is-collapsed { transform: rotate(-90deg); }
.review-fork__name { font-weight: 600; font-size: 0.8rem; }
.review-fork__count { font-size: 0.72rem; color: var(--text-muted); }

.review-fork__thread { padding: 0.55rem 0.6rem; display: flex; flex-direction: column; gap: 0.55rem; }
.review-msg {
  border-left: 2px solid var(--border);
  padding-left: 0.6rem;
}
.review-msg--critic { border-left-color: var(--err); }
.review-msg--proposer { border-left-color: var(--ok); }
.review-msg__head {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  margin-bottom: 0.2rem;
}
.review-msg__role {
  font-size: 0.68rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.review-msg--critic .review-msg__role { color: var(--err); }
.review-msg--proposer .review-msg__role { color: var(--ok); }
.review-msg__round { font-size: 0.7rem; color: var(--text-muted); }
.review-msg__body { font-size: 0.82rem; }

/* Round bodies are full markdown documents; tame their headings so a leading
   "# Critic 1 …" doesn't render as a page-sized title inside the thread. */
.review-md :deep(h1),
.review-md :deep(h2),
.review-md :deep(h3),
.review-md :deep(h4) {
  font-size: 0.85rem;
  font-weight: 600;
  margin: 0.5rem 0 0.25rem;
  line-height: 1.3;
}
.review-md :deep(p) { margin: 0.3rem 0; }
.review-md :deep(pre) { font-size: 0.75rem; }

.review__empty {
  margin-top: 0.7rem;
  font-size: 0.78rem;
  color: var(--text-muted);
}
</style>
