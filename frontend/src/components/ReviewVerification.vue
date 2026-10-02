<script setup lang="ts">
import { computed } from 'vue';
import type { Task, ReviewTranscript, ReviewFinding } from '../api/types';
import { renderMarkdown } from '../lib/markdown';

const props = defineProps<{ task: Task; transcript: ReviewTranscript | null }>();

const running = computed(() => props.transcript?.running ?? false);
const legacy = computed(() => props.transcript?.legacy ?? false);
const config = computed(() => props.transcript?.config ?? null);
const outcome = computed(() => props.transcript?.outcome ?? null);
const rounds = computed(() => props.transcript?.rounds ?? []);
// Set when the server could not read the transcript to its end: rounds holds
// the rounds before that point and later rounds are missing.
const truncated = computed(() => props.transcript?.truncated ?? false);

const unresolved = computed(() => props.task.review_unresolved);
// A review exists when there is a session on disk or a verdict on the task.
const hasRun = computed(() => props.transcript !== null || unresolved.value !== undefined);

type StatusKind = 'running' | 'clean' | 'issues' | 'pending' | 'idle';
const status = computed<{ label: string; kind: StatusKind }>(() => {
  if (running.value) return { label: 'Running', kind: 'running' };
  if (unresolved.value === 0) return { label: 'Approved', kind: 'clean' };
  if (unresolved.value !== undefined) return { label: `${unresolved.value} open`, kind: 'issues' };
  if (outcome.value?.termination === 'skipped') return { label: 'Did not run', kind: 'idle' };
  // A session without an outcome is between rounds: the task is taking its
  // turn on the findings, or a failed attempt waits for its retry.
  if (props.transcript && !legacy.value && !outcome.value) return { label: 'In progress', kind: 'pending' };
  return { label: 'Not run', kind: 'idle' };
});

const reviewCost = computed(() => props.task.usage_breakdown?.review?.cost_usd ?? 0);

const outcomeText = computed(() => {
  const o = outcome.value;
  if (!o) return '';
  const n = o.unresolved;
  const findings = `${n} open finding${n === 1 ? '' : 's'}`;
  switch (o.termination) {
    case 'approved':
      return 'Approved: the reviewer found nothing that must change.';
    case 'max_rounds':
      return `${findings} after the last round.`;
    case 'cost_cap':
      return `The reviewer token budget is spent, with ${findings}.`;
    case 'skipped':
      return o.skip?.message ?? 'The review did not run.';
    case 'superseded':
      return 'The task moved on while the reviewer ran, so its findings were not sent.';
    case 'unreadable':
      return "Part of this review's record could not be read; the next review starts a new one.";
    default:
      return 'The review has ended.';
  }
});

const outcomeKind = computed<StatusKind>(() => {
  const o = outcome.value;
  if (!o) return 'idle';
  if (o.termination === 'approved') return 'clean';
  if (o.unresolved > 0) return 'issues';
  return 'idle';
});

function fmtTokens(n: number): string {
  if (!n) return '';
  return n >= 1000 ? `${(n / 1000).toFixed(1)}k tokens` : `${n} tokens`;
}

function verdictLabel(v: string): string {
  if (v === 'approve') return 'Approved';
  if (v === 'changes_requested') return 'Changes requested';
  return v;
}

function findingKey(f: ReviewFinding, i: number): string {
  return `${i}-${f.severity}-${f.claim}`;
}
</script>

<template>
  <section class="review">
    <header class="review__header">
      <div class="review__title">
        <span class="review__icon" aria-hidden="true">&#9878;</span>
        <span>Review</span>
      </div>
      <span class="review__status" :class="`review__status--${status.kind}`">
        <span v-if="status.kind === 'running'" class="review__dot" aria-hidden="true">&#9679;</span>
        {{ status.label }}
      </span>
    </header>

    <p v-if="config" class="review__config">
      <template v-if="config.reviewer_model">Reviewer <strong>{{ config.reviewer_model }}</strong></template>
      <template v-else>No reviewer model is set</template>
      · up to {{ config.max_rounds }} round{{ config.max_rounds === 1 ? '' : 's' }}
      · budget {{ Math.round(config.cost_cap / 1000) }}k tokens
    </p>

    <p v-if="legacy" class="review__legacy" role="status">
      This review was recorded by an earlier version of Wallfacer, and its rounds cannot be shown here.
    </p>

    <!-- Outcome of an ended session -->
    <div v-if="outcome && !running" class="review__outcome" :class="`review__outcome--${outcomeKind}`">
      <div class="review__verdict">{{ outcomeText }}</div>
      <p v-if="outcome.headline && outcome.unresolved > 0" class="review__headline">{{ outcome.headline }}</p>
      <div class="review__meta">
        <span v-if="outcome.rounds">{{ outcome.rounds }} round{{ outcome.rounds === 1 ? '' : 's' }}</span>
        <span v-if="outcome.tokens">{{ fmtTokens(outcome.tokens) }}</span>
        <span v-if="reviewCost > 0">${{ reviewCost.toFixed(2) }}</span>
      </div>
    </div>
    <!-- A verdict on the task without a readable outcome (an earlier version's run) -->
    <div v-else-if="!running && unresolved !== undefined && !outcome" class="review__outcome" :class="`review__outcome--${unresolved === 0 ? 'clean' : 'issues'}`">
      <div class="review__verdict">
        <template v-if="unresolved === 0">Approved: no open findings.</template>
        <template v-else>{{ unresolved }} open finding{{ unresolved === 1 ? '' : 's' }}.</template>
      </div>
      <p v-if="task.review_headline && (unresolved ?? 0) > 0" class="review__headline">{{ task.review_headline }}</p>
    </div>

    <p v-if="truncated" class="review__truncated" role="status">
      This transcript is incomplete: part of it could not be read, and later rounds are not shown.
    </p>

    <!-- Rounds: the reviewer's answer, then the task's turn on its findings -->
    <div v-if="rounds.length" class="review__rounds">
      <section v-for="rd in rounds" :key="`round-${rd.round}`" class="review-round">
        <h4 class="review-round__title">Round {{ rd.round }}</h4>

        <div v-for="(a, i) in rd.failed_attempts ?? []" :key="`fail-${rd.round}-${i}`" class="review-round__failed">
          {{ a.message }}
          <details v-if="a.raw" class="review-round__raw">
            <summary>Reviewer output</summary>
            <pre>{{ a.raw }}</pre>
          </details>
        </div>

        <article v-if="rd.reviewer" class="review-msg review-msg--reviewer">
          <header class="review-msg__head">
            <span class="review-msg__role">Reviewer</span>
            <span class="review-verdict" :class="`review-verdict--${rd.reviewer.verdict}`">{{ verdictLabel(rd.reviewer.verdict) }}</span>
            <span v-if="rd.reviewer.model" class="review-msg__model">{{ rd.reviewer.model }}</span>
          </header>
          <ul v-if="rd.reviewer.findings.length" class="review-findings">
            <li v-for="(f, i) in rd.reviewer.findings" :key="findingKey(f, i)" class="review-finding">
              <span class="review-sev" :class="`review-sev--${f.severity}`">{{ f.severity }}</span>
              <span class="review-finding__claim">{{ f.claim }}</span>
              <code v-if="f.location" class="review-finding__loc">{{ f.location }}</code>
            </li>
          </ul>
          <p v-if="rd.reviewer.summary" class="review-msg__summary">{{ rd.reviewer.summary }}</p>
        </article>

        <details v-if="rd.feedback" class="review-msg review-msg--feedback">
          <summary class="review-msg__role">Sent to the task as feedback</summary>
          <pre class="review-msg__pre">{{ rd.feedback }}</pre>
        </details>

        <article v-if="rd.reply" class="review-msg review-msg--implementer">
          <header class="review-msg__head"><span class="review-msg__role">Task's reply</span></header>
          <!-- eslint-disable-next-line vue/no-v-html — renderMarkdown sanitizes -->
          <div class="review-msg__body prose-content review-md" v-html="renderMarkdown(rd.reply)" />
        </article>
      </section>
    </div>

    <div v-else-if="running" class="review__empty">Review running, waiting for the reviewer's answer.</div>
    <div v-else-if="!hasRun" class="review__empty">
      No review has run for this task yet. Trigger <strong>Review</strong> from the actions panel.
    </div>
  </section>
</template>

<style scoped>
.review {
  border: 1px solid var(--rule);
  border-radius: var(--r-lg);
  background: var(--bg-card);
  padding: 12px 14px;
  margin-bottom: 16px;
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
  border-radius: var(--r-pill);
  white-space: nowrap;
}
.review__status--running { color: var(--run); background: var(--tint-blue); }
.review__status--pending { color: var(--run); background: var(--tint-blue); }
.review__status--clean { color: var(--ok); background: var(--tint-green); }
.review__status--issues { color: var(--warn); background: var(--tint-amber); }
.review__status--idle { color: var(--ink-3); background: var(--tint-neutral); }
.review__dot { font-size: 0.6rem; animation: review-pulse 1.4s ease-in-out infinite; }
@keyframes review-pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.3; } }

.review__config {
  margin: 0.55rem 0 0;
  font-size: 0.74rem;
  color: var(--ink-2);
  line-height: 1.5;
}
.review__config strong { color: var(--ink); font-weight: 600; }

.review__legacy,
.review__truncated {
  margin: 0.75rem 0 0;
  padding: 0.45rem 0.6rem;
  border-radius: var(--r-sm);
  font-size: 0.78rem;
}
.review__legacy { color: var(--ink-2); background: var(--tint-neutral); }
.review__truncated { color: var(--warn); background: var(--tint-amber); }

.review__outcome {
  margin-top: 0.75rem;
  padding: 0.6rem 0.7rem;
  border-radius: var(--r-sm);
  border-left: 3px solid var(--rule-2);
  background: var(--bg-sunk);
}
.review__outcome--clean { border-left-color: var(--ok); }
.review__outcome--issues { border-left-color: var(--warn); }
.review__verdict { font-size: 0.82rem; font-weight: 600; }
.review__headline { margin: 0.35rem 0 0; font-size: 0.8rem; }
.review__meta {
  display: flex;
  flex-wrap: wrap;
  gap: 0.3rem 0.75rem;
  margin-top: 0.45rem;
  font-size: 0.72rem;
  color: var(--ink-3);
}

.review__rounds { margin-top: 0.85rem; display: flex; flex-direction: column; gap: 0.75rem; }
.review-round {
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  padding: 0.55rem 0.6rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.review-round__title { margin: 0; font-size: 0.8rem; font-weight: 600; }
.review-round__failed { margin: 0; font-size: 0.78rem; color: var(--warn); }
.review-round__raw { margin-top: 0.25rem; color: var(--ink-2); }
.review-round__raw pre,
.review-msg__pre {
  margin: 0.3rem 0 0;
  font-size: 0.74rem;
  white-space: pre-wrap;
  word-break: break-word;
}

.review-msg {
  border-left: 2px solid var(--rule-2);
  padding-left: 0.6rem;
}
.review-msg--reviewer { border-left-color: var(--accent); }
.review-msg--feedback { border-left-color: var(--rule-2); }
.review-msg--implementer { border-left-color: var(--ok); }
.review-msg__head {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0.45rem;
  margin-bottom: 0.25rem;
}
.review-msg__role {
  font-size: 0.68rem;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  color: var(--ink-2);
}
summary.review-msg__role { cursor: pointer; }
.review-msg__model { font-size: 0.7rem; color: var(--ink-3); }
.review-msg__summary { margin: 0.3rem 0 0; font-size: 0.78rem; color: var(--ink-2); }
.review-msg__body { font-size: 0.82rem; }

.review-verdict {
  font-size: 0.68rem;
  font-weight: 600;
  padding: 0.05rem 0.4rem;
  border-radius: var(--r-pill);
}
.review-verdict--approve { color: var(--ok); background: var(--tint-green); }
.review-verdict--changes_requested { color: var(--warn); background: var(--tint-amber); }

.review-findings { margin: 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 0.3rem; }
.review-finding { display: flex; flex-wrap: wrap; align-items: baseline; gap: 0.4rem; font-size: 0.8rem; }
.review-finding__loc { font-size: 0.72rem; color: var(--ink-3); }
.review-sev {
  font-size: 0.64rem;
  font-weight: 700;
  text-transform: uppercase;
  padding: 0.02rem 0.35rem;
  border-radius: var(--r-sm);
  color: var(--ink-2);
  background: var(--tint-neutral);
}
.review-sev--high { color: var(--err); background: var(--tint-red); }
.review-sev--medium { color: var(--warn); background: var(--tint-amber); }

/* Replies are full markdown documents; tame their headings so a leading
   heading doesn't render as a page-sized title inside the round. */
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
  color: var(--ink-3);
}
</style>
