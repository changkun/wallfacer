<script setup lang="ts">
import { computed, ref } from 'vue';
import type { Task, AgonTranscript } from '../api/types';
import { renderMarkdown } from '../lib/markdown';

const props = defineProps<{ task: Task; transcript: AgonTranscript | null }>();

const running = computed(() => props.transcript?.running ?? false);
const config = computed(() => props.transcript?.config ?? null);
const outcome = computed(() => props.transcript?.outcome ?? null);
const forks = computed(() => props.transcript?.forks ?? []);

// A run exists if there is a live trajectory or a persisted verdict on the task.
const hasRun = computed(() => forks.value.length > 0 || props.task.agon_unresolved !== undefined);
const unresolved = computed(() => props.task.agon_unresolved);

const status = computed<{ label: string; kind: 'running' | 'clean' | 'issues' | 'idle' }>(() => {
  if (running.value) return { label: 'Running', kind: 'running' };
  if (unresolved.value === undefined) return { label: 'Not run', kind: 'idle' };
  if (unresolved.value === 0) return { label: 'Clean', kind: 'clean' };
  return { label: `${unresolved.value} unresolved`, kind: 'issues' };
});

const agonCost = computed(() => props.task.usage_breakdown?.agon?.cost_usd ?? 0);

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
  <section class="agon">
    <header class="agon__header">
      <div class="agon__title">
        <span class="agon__icon" aria-hidden="true">&#9878;</span>
        <span>Adversarial Verification</span>
      </div>
      <span class="agon__status" :class="`agon__status--${status.kind}`">
        <span v-if="status.kind === 'running'" class="agon__dot" aria-hidden="true">&#9679;</span>
        {{ status.label }}
      </span>
    </header>

    <p v-if="config" class="agon__config">
      {{ config.forks }} critic fork{{ config.forks === 1 ? '' : 's' }} · up to {{ config.max_rounds }} rounds each ·
      proposer <strong>{{ config.proposer_model }}</strong> ·
      critics <strong>{{ config.critic_models.join(', ') }}</strong> ·
      budget {{ Math.round(config.cost_cap / 1000) }}k tokens
    </p>

    <!-- Outcome (after a completed run) -->
    <div v-if="!running && hasRun" class="agon__outcome" :class="`agon__outcome--${status.kind}`">
      <div class="agon__verdict">
        <template v-if="status.kind === 'clean'">No unresolved attacks — the changes survived the debate.</template>
        <template v-else-if="status.kind === 'issues'">{{ unresolved }} unresolved attack{{ unresolved === 1 ? '' : 's' }} remain.</template>
        <template v-else>Verification complete.</template>
      </div>
      <!-- eslint-disable-next-line vue/no-v-html — renderMarkdown sanitises -->
      <div
        v-if="task.agon_headline && (unresolved ?? 0) > 0"
        class="agon__headline prose-content agon-md"
        v-html="renderMarkdown(task.agon_headline)"
      />
      <div class="agon__meta">
        <span v-if="byStatusText">{{ byStatusText }}</span>
        <span v-if="outcome?.termination">ended: {{ outcome.termination.replace(/_/g, ' ') }}</span>
        <span v-if="outcome && outcome.wall_seconds">{{ fmtDuration(outcome.wall_seconds) }}</span>
        <span v-if="outcome && outcome.tokens">{{ fmtTokens(outcome.tokens) }}</span>
        <span v-if="agonCost > 0">${{ agonCost.toFixed(2) }}</span>
      </div>
    </div>

    <!-- Trajectory: each fork is a debate thread -->
    <div v-if="forks.length" class="agon__forks">
      <section v-for="fork in forks" :key="`fork-${fork.index}`" class="agon-fork">
        <button type="button" class="agon-fork__head" @click="toggleFork(fork.index)">
          <span class="agon-fork__chevron" :class="{ 'is-collapsed': collapsed[fork.index] }" aria-hidden="true">&#9662;</span>
          <span class="agon-fork__name">Fork {{ fork.index }}</span>
          <span class="agon-fork__count">{{ fork.rounds.length }} round{{ fork.rounds.length === 1 ? '' : 's' }}</span>
        </button>
        <div v-show="!collapsed[fork.index]" class="agon-fork__thread">
          <article
            v-for="r in fork.rounds"
            :key="`r-${fork.index}-${r.round}-${r.role}`"
            class="agon-msg"
            :class="`agon-msg--${r.role}`"
          >
            <header class="agon-msg__head">
              <span class="agon-msg__role">{{ r.role === 'critic' ? 'Critic' : 'Proposer' }}</span>
              <span class="agon-msg__round">Round {{ r.round }}</span>
            </header>
            <!-- eslint-disable-next-line vue/no-v-html — renderMarkdown sanitises -->
            <div class="agon-msg__body prose-content agon-md" v-html="renderMarkdown(r.body)" />
          </article>
        </div>
      </section>
    </div>

    <div v-else-if="running" class="agon__empty">Verification running… waiting for the first round to land.</div>
    <div v-else-if="!hasRun" class="agon__empty">
      No adversarial verification has run for this task yet. Trigger <strong>Agon</strong> from the actions panel.
    </div>
  </section>
</template>

<style scoped>
.agon {
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-elevated);
  padding: 0.85rem;
  margin-bottom: 1.25rem;
}
.agon__header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 0.5rem;
}
.agon__title {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  font-weight: 600;
  font-size: 0.9rem;
}
.agon__icon { color: var(--accent); font-size: 1.05rem; }
.agon__status {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  font-size: 0.72rem;
  font-weight: 600;
  padding: 0.15rem 0.5rem;
  border-radius: 999px;
  white-space: nowrap;
}
.agon__status--running { color: var(--accent); background: color-mix(in srgb, var(--accent) 14%, transparent); }
.agon__status--clean { color: var(--ok); background: color-mix(in srgb, var(--ok) 16%, transparent); }
.agon__status--issues { color: var(--warn); background: color-mix(in srgb, var(--warn) 18%, transparent); }
.agon__status--idle { color: var(--text-muted); background: var(--bg-hover); }
.agon__dot { font-size: 0.6rem; animation: agon-pulse 1.4s ease-in-out infinite; }
@keyframes agon-pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.3; } }

.agon__config {
  margin: 0.55rem 0 0;
  font-size: 0.74rem;
  color: var(--text-secondary);
  line-height: 1.5;
}
.agon__config strong { color: var(--text); font-weight: 600; }

.agon__outcome {
  margin-top: 0.75rem;
  padding: 0.6rem 0.7rem;
  border-radius: 8px;
  border-left: 3px solid var(--border);
  background: var(--bg-sunk);
}
.agon__outcome--clean { border-left-color: var(--ok); }
.agon__outcome--issues { border-left-color: var(--warn); }
.agon__verdict { font-size: 0.82rem; font-weight: 600; }
.agon__headline { margin-top: 0.35rem; font-size: 0.8rem; }
.agon__meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem 0.75rem;
  margin-top: 0.45rem;
  font-size: 0.72rem;
  color: var(--text-muted);
}

.agon__forks { margin-top: 0.85rem; display: flex; flex-direction: column; gap: 0.6rem; }
.agon-fork {
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
  background: var(--bg-card);
}
.agon-fork__head {
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
.agon-fork__chevron {
  font-size: 0.7rem;
  color: var(--text-muted);
  transition: transform 0.15s ease;
  line-height: 1;
}
.agon-fork__chevron.is-collapsed { transform: rotate(-90deg); }
.agon-fork__name { font-weight: 600; font-size: 0.8rem; }
.agon-fork__count { font-size: 0.72rem; color: var(--text-muted); }

.agon-fork__thread { padding: 0.55rem 0.6rem; display: flex; flex-direction: column; gap: 0.55rem; }
.agon-msg {
  border-left: 2px solid var(--border);
  padding-left: 0.6rem;
}
.agon-msg--critic { border-left-color: var(--err); }
.agon-msg--proposer { border-left-color: var(--ok); }
.agon-msg__head {
  display: flex;
  align-items: center;
  gap: 0.45rem;
  margin-bottom: 0.2rem;
}
.agon-msg__role {
  font-size: 0.68rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.agon-msg--critic .agon-msg__role { color: var(--err); }
.agon-msg--proposer .agon-msg__role { color: var(--ok); }
.agon-msg__round { font-size: 0.7rem; color: var(--text-muted); }
.agon-msg__body { font-size: 0.82rem; }

/* Round bodies are full markdown documents; tame their headings so a leading
   "# Critic 1 …" doesn't render as a page-sized title inside the thread. */
.agon-md :deep(h1),
.agon-md :deep(h2),
.agon-md :deep(h3),
.agon-md :deep(h4) {
  font-size: 0.85rem;
  font-weight: 600;
  margin: 0.5rem 0 0.25rem;
  line-height: 1.3;
}
.agon-md :deep(p) { margin: 0.3rem 0; }
.agon-md :deep(pre) { font-size: 0.75rem; }

.agon__empty {
  margin-top: 0.7rem;
  font-size: 0.78rem;
  color: var(--text-muted);
}
</style>
