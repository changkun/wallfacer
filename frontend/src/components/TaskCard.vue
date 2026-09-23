<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useTaskStore } from '../stores/tasks';
import { useUiStore } from '../stores/ui';
import { api } from '../api/client';
import type { Task } from '../api/types';
import { renderMarkdown } from '../lib/markdown';
import { highlightMatch } from '../lib/highlight';
import { orderTags } from '../lib/tagBadge';
import { cardActionsFor, primaryCardAction, CARD_ACTION_DEFS, type CardAction } from '../lib/cardActions';
import AppSelect from './AppSelect.vue';
import HarnessLogo from './HarnessLogo.vue';
import { dependencyBadge, failureLabel } from '../lib/cardBadges';
import { useGithubPrStore } from '../stores/githubPr';
import { useBehindCounts } from '../composables/useBehindCounts';
import { useNow } from '../composables/useNow';
import { routineCountdown as routineCountdownFor, routineLastFired as routineLastFiredFor } from '../lib/routineTime';
import { toRef } from 'vue';

const props = defineProps<{ task: Task; rank?: number }>();

const ROUTINE_INTERVAL_OPTIONS = [1, 5, 15, 30, 60, 180, 360, 720, 1440];

const isRoutine = computed(() => props.task.kind === 'routine');

const routineMinutes = computed(() => {
  const sec = props.task.routine_interval_seconds || 0;
  return Math.max(1, Math.round(sec / 60));
});

const routineEnabled = computed(() => !!props.task.routine_enabled);

const routineSpawnLabel = computed(() => {
  return props.task.routine_spawn_flow || props.task.routine_spawn_kind || 'task';
});

const routineIntervalChoices = computed(() => {
  const set = new Set<number>(ROUTINE_INTERVAL_OPTIONS);
  if (routineMinutes.value > 0) set.add(routineMinutes.value);
  return Array.from(set).sort((a, b) => a - b);
});
const routineIntervalOptions = computed(() =>
  routineIntervalChoices.value.map((m) => ({ value: m, label: `${m} min` })),
);

// Shared 1s clock: one interval across all cards rather than one per card.
const now = useNow();

// One-shot "just created" pulse for cards freshly dispatched from Plan mode.
const uiStore = useUiStore();
const justDispatched = ref(false);
let pulseHandle: ReturnType<typeof setTimeout> | null = null;

const prStore = useGithubPrStore();

// PR signal: the task branch's PR state. Reads the per-task PR cache;
// populated by the fetch below for branched tasks.
const prBadge = computed<{ label: string; cls: string; title: string } | null>(() => {
  const pr = prStore.prFor(props.task.id);
  if (!pr) return null;
  const cls = pr.state === 'open' ? 'pill-ok' : pr.state === 'merged' ? 'pill-pub' : 'pill-err';
  return { label: `#${pr.number} ${pr.state}`, cls, title: `Pull request #${pr.number} (${pr.state})` };
});

onMounted(() => {
  if (uiStore.dispatchedIds.has(props.task.id)) {
    justDispatched.value = true;
    pulseHandle = setTimeout(() => {
      justDispatched.value = false;
      uiStore.consumeDispatched(props.task.id);
    }, 1500);
  }
  // Populate the PR badge for tasks that have a branch (only a subset of the
  // board), so the card can show PR state without opening the task.
  if (props.task.branch_name && prStore.prFor(props.task.id) === undefined) {
    void prStore.fetchTaskPR(props.task.id);
  }
});

onBeforeUnmount(() => {
  if (pulseHandle !== null) clearTimeout(pulseHandle);
});

const routineCountdown = computed(() => routineCountdownFor(props.task, now.value));
const routineLastFired = computed(() => routineLastFiredFor(props.task, now.value));

async function onRoutineIntervalChange(minutes: number) {
  if (!Number.isFinite(minutes) || minutes < 1) return;
  try {
    await api('PATCH', `/api/routines/${props.task.id}/schedule`, { interval_minutes: minutes });
  } catch (err) {
    console.error('Error updating routine interval:', err);
  }
}

async function onRoutineEnabledChange(e: Event) {
  const target = e.target as HTMLInputElement;
  try {
    await api('PATCH', `/api/routines/${props.task.id}/schedule`, { enabled: target.checked });
  } catch (err) {
    console.error('Error toggling routine:', err);
  }
}

async function onRoutineTrigger(e: Event) {
  e.stopPropagation();
  try {
    await api('POST', `/api/routines/${props.task.id}/trigger`);
  } catch (err) {
    console.error('Error triggering routine:', err);
  }
}

function statusLabel(task: Task): string {
  if (task.archived) return 'archived';
  if (task.status === 'in_progress') return 'in progress';
  if (task.status === 'committing') return 'committing';
  return task.status;
}

// The state pill: one ramp color per column, a dot on the live states.
const statePill = computed<{ cls: string; dot: boolean; pulse: boolean }>(() => {
  const t = props.task;
  if (t.archived) return { cls: 'pill-neutral', dot: false, pulse: false };
  switch (t.status) {
    case 'in_progress':
    case 'committing': return { cls: 'pill-run', dot: true, pulse: true };
    case 'waiting': return { cls: 'pill-warn', dot: true, pulse: false };
    case 'done': return { cls: 'pill-ok', dot: false, pulse: false };
    case 'failed': return { cls: 'pill-err', dot: true, pulse: false };
    default: return { cls: 'pill-neutral', dot: false, pulse: false };
  }
});

function cardClasses(task: Task): Record<string, boolean> {
  return {
    'task-card': true,
    [`task-card--${task.archived ? 'archived' : task.status}`]: true,
    'task-card--routine': task.kind === 'routine',
    'task-card--just-created': justDispatched.value,
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

// Behind-upstream chip: fetched lazily via the shared composable, only
// for statuses where falling behind matters. Routine cards opt out.
const showsBehind = computed(() =>
  !isRoutine.value && (props.task.status === 'waiting' || props.task.status === 'failed'),
);
const behind = useBehindCounts(
  computed(() => (showsBehind.value ? props.task.id : '')),
  toRef(props.task, 'updated_at'),
);

async function syncFromCard(e: Event) {
  e.stopPropagation();
  try {
    await api('POST', '/api/git/sync', { task_id: props.task.id });
  } catch { /* handled by SSE state refresh */ }
}

// Test-verification badge for the card's Row 1. Mirrors ui/js/render.js:
// pass → ✓ verified; fail → ✗ verify failed; unknown → no verdict; an
// untested waiting task → unverified. Other statuses get nothing.
const testBadge = computed<{ label: string; cls: string; title: string } | null>(() => {
  const t = props.task;
  switch (t.last_test_result) {
    case 'pass': return { label: 'verified', cls: 'pill-ok', title: 'Verification passed' };
    case 'fail': return { label: 'verify failed', cls: 'pill-err', title: 'Verification failed' };
    case 'unknown': return { label: 'no verdict', cls: 'pill-neutral', title: 'Tested — no clear verdict detected' };
    default:
      if (t.status === 'waiting') {
        return { label: 'unverified', cls: 'pill-neutral', title: 'Not yet verified' };
      }
      return null;
  }
});

// Tags as one meta line: priority, impact, labels, provenance.
const tags = computed(() => orderTags(props.task.tags ?? []));

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

const cardActions = computed(() =>
  cardActionsFor(props.task).map((id) => CARD_ACTION_DEFS[id]),
);
const primaryAction = computed(() => primaryCardAction(props.task));

const router = useRouter();
const taskStore = useTaskStore();

// Highlight the active board filter inside the card title (legacy parity).
const titleHtml = computed(() => highlightMatch(props.task.title || '', taskStore.filterQuery));

// Dependency-state badge for backlog cards (blocked / ready / cancelled),
// resolved against the live task list. Mirrors render.js renderDependencyBadge.
const depBadge = computed(() => {
  // Only backlog cards carry a dependency badge; skip the map lookup entirely
  // for every other card so a tasks change does not touch them.
  if (props.task.status !== 'backlog') return null;
  return dependencyBadge(props.task, taskStore.tasksById);
});
const depBadgeClass = computed(() => {
  switch (depBadge.value?.kind) {
    case 'blocked': return 'pill-warn';
    case 'ready': return 'pill-ok';
    case 'cancelled': return 'pill-warn';
    default: return '';
  }
});
const depBadgeText = computed(() => {
  const b = depBadge.value;
  if (!b) return '';
  if (b.kind === 'ready') return 'ready';
  if (b.kind === 'cancelled') return 'dependency cancelled';
  return `${b.count} dep${b.count !== 1 ? 's' : ''}`;
});
const depBadgeTitle = computed(() => {
  const b = depBadge.value;
  if (!b) return '';
  if (b.kind === 'blocked') return `Blocked by: ${b.blocking}`;
  if (b.kind === 'cancelled') return 'A dependency was cancelled or removed; this task may be unblocked after the next sync';
  return 'All dependencies satisfied; ready for promotion';
});

// Friendly failure-category label (Timeout/Budget/…) for failed cards.
const failureBadge = computed(() => failureLabel(props.task.failure_category));

// The card's color budget: one state pill and one qualifier pill on the first
// row. The qualifier is the most decisive signal available; the rest become
// plain text on the meta line so nothing is lost, only de-emphasized.
interface Signal { label: string; cls: string; title: string }
const signals = computed<Signal[]>(() => {
  const out: Signal[] = [];
  if (testBadge.value) out.push(testBadge.value);
  if (failureBadge.value) out.push({ label: failureBadge.value, cls: 'pill-err', title: 'Failure reason: ' + props.task.failure_category });
  if (depBadge.value) out.push({ label: depBadgeText.value, cls: depBadgeClass.value, title: depBadgeTitle.value });
  if (scheduledLabel.value) out.push({ label: scheduledLabel.value, cls: 'pill-neutral', title: 'Scheduled start' });
  if (prBadge.value) out.push(prBadge.value);
  return out;
});
const qualifier = computed(() => signals.value[0] ?? null);
const extraSignals = computed(() => signals.value.slice(1));

// Cost budget progress bar on running/waiting cards.
const costBar = computed(() => {
  const t = props.task;
  const max = t.max_cost_usd ?? 0;
  if (!(max > 0) || !(t.status === 'in_progress' || t.status === 'waiting')) return null;
  const spent = t.usage?.cost_usd ?? 0;
  const pct = Math.min(100, (spent / max) * 100);
  const color = pct >= 90 ? 'var(--err)' : pct >= 70 ? 'var(--warn)' : 'var(--ok)';
  return { pct, color, title: `Cost: $${spent.toFixed(4)} of $${max.toFixed(2)} budget` };
});

// Scheduled badge with relative time on backlog cards (one-shot scheduled_at).
const scheduledLabel = computed(() => {
  const t = props.task;
  if (t.status !== 'backlog' || !t.scheduled_at) return '';
  const at = new Date(t.scheduled_at).getTime();
  if (Number.isNaN(at)) return '';
  const diff = at - now.value;
  if (diff <= 0) return 'scheduled';
  const m = Math.round(diff / 60000);
  if (m < 60) return `in ${m}m`;
  const h = Math.round(m / 60);
  if (h < 24) return `in ${h}h`;
  return `in ${Math.round(h / 24)}d`;
});

// Clicking a tag chip filters the board to that exact tag using the
// `#tag` search prefix matchesFilter understands. stopPropagation keeps
// the row click from also opening the task detail.
function filterByTag(tag: string, e: Event) {
  e.stopPropagation();
  taskStore.filterQuery = ('#' + tag).toLowerCase();
}

async function runCardAction(action: CardAction, e: Event) {
  e.stopPropagation();
  const id = props.task.id;
  switch (action) {
    case 'plan': router.push({ path: '/plan', query: { task: id } }); break;
    case 'start': await api('PATCH', `/api/tasks/${id}`, { status: 'in_progress' }); break;
    case 'retry': await api('PATCH', `/api/tasks/${id}`, { status: 'backlog' }); break;
    case 'done': await api('POST', `/api/tasks/${id}/done`); break;
    case 'resume': await api('POST', `/api/tasks/${id}/resume`); break;
    case 'test': await api('POST', `/api/tasks/${id}/test`); break;
  }
}

const cardRoot = ref<HTMLDivElement | null>(null);

function focusSibling(direction: 'next' | 'prev' | 'left' | 'right') {
  const root = cardRoot.value;
  if (!root) return;
  // For vertical nav we walk siblings in the same column; for horizontal
  // we walk by position across columns. Use a flat DOM query and find
  // ourselves in it — simple, side-effect-free.
  // `.task-card` is the root (see cardClasses()); a wrong selector here
  // silently breaks card-to-card keyboard nav.
  const all = Array.from(document.querySelectorAll<HTMLElement>('.task-card[tabindex="0"]'));
  const idx = all.indexOf(root);
  if (idx < 0 || all.length === 0) return;
  let nextIdx = idx;
  if (direction === 'next') nextIdx = Math.min(all.length - 1, idx + 1);
  else if (direction === 'prev') nextIdx = Math.max(0, idx - 1);
  else {
    // Left/Right: find the nearest card whose column differs and whose
    // vertical center is closest to ours.
    const me = root.getBoundingClientRect();
    const myCol = me.left + me.width / 2;
    let bestIdx = idx;
    let bestDist = Infinity;
    for (let i = 0; i < all.length; i++) {
      if (i === idx) continue;
      const r = all[i].getBoundingClientRect();
      const isLeft = r.right < me.left;
      const isRight = r.left > me.right;
      if (direction === 'left' && !isLeft) continue;
      if (direction === 'right' && !isRight) continue;
      const dx = Math.abs((r.left + r.width / 2) - myCol);
      const dy = Math.abs((r.top + r.height / 2) - (me.top + me.height / 2));
      const d = dx + dy;
      if (d < bestDist) { bestDist = d; bestIdx = i; }
    }
    nextIdx = bestIdx;
  }
  if (nextIdx !== idx) all[nextIdx].focus();
}

function onCardKeydown(e: KeyboardEvent) {
  // Don't hijack typing inside the routine footer's interval picker + spawn icon.
  const target = e.target as HTMLElement;
  if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.tagName === 'SELECT') return;
  if (target.closest('.app-select')) return;

  if (e.key === 'Enter' || e.key === ' ') {
    e.preventDefault();
    cardRoot.value?.click();
    return;
  }
  if (e.key === 'Escape') {
    e.preventDefault();
    cardRoot.value?.blur();
    return;
  }
  if (e.key === 'ArrowDown') { e.preventDefault(); focusSibling('next'); return; }
  if (e.key === 'ArrowUp')   { e.preventDefault(); focusSibling('prev'); return; }
  if (e.key === 'ArrowLeft') { e.preventDefault(); focusSibling('left'); return; }
  if (e.key === 'ArrowRight'){ e.preventDefault(); focusSibling('right'); return; }

  // Per-status quick actions. Only fire when the action is in the matrix
  // for this card's current status (e.g. "s" → start on backlog only).
  if (e.key === 's' || e.key === 'd' || e.key === 'r' || e.key === 't' || e.key === 'p') {
    const wanted: CardAction | null =
      e.key === 's' ? 'start'
        : e.key === 'd' ? 'done'
        : e.key === 'r' ? (props.task.session_id && props.task.status === 'waiting' ? 'resume' : 'retry')
        : e.key === 't' ? 'test'
        : 'plan';
    if (wanted && cardActionsFor(props.task).includes(wanted)) {
      e.preventDefault();
      void runCardAction(wanted, e);
    }
  }
}
</script>

<template>
  <div
    ref="cardRoot"
    :class="cardClasses(props.task)"
    tabindex="0"
    role="button"
    :aria-label="`Task: ${props.task.title || props.task.prompt || props.task.id}`"
    @keydown="onCardKeydown"
  >
    <!-- Row 1: rank, state, one qualifier; harness · timeout · age on the right -->
    <div class="task-card__row">
      <div class="task-card__pills">
        <span v-if="props.rank" class="pill pill-neutral task-card__rank" title="Backlog position">#{{ props.rank }}</span>
        <span :class="['pill', statePill.cls, { pulse: statePill.pulse }]" data-role="state">
          <span v-if="statePill.dot" class="pill-dot" aria-hidden="true" />{{ statusLabel(props.task) }}
        </span>
        <span v-if="qualifier" :class="['pill', qualifier.cls]" :title="qualifier.title" data-role="qualifier">{{ qualifier.label }}</span>
      </div>
      <div class="task-card__meta">
        <span class="task-card__harness" :title="'Harness: ' + sandboxLabel(props.task)">
          <HarnessLogo :harness="props.task.sandbox || 'claude'" :size="12" />{{ sandboxLabel(props.task) }}
        </span>
        <span title="Timeout">{{ formatTimeout(props.task.timeout) }}</span>
        <span :title="'Created ' + props.task.created_at">{{ timeAgo(props.task.created_at) }}</span>
      </div>
    </div>

    <!-- Row 2: title -->
    <div v-if="props.task.title_generating" class="task-card__title" role="status" aria-live="polite">
      <span class="spinner" aria-hidden="true"></span> Generating title…
    </div>
    <div v-if="props.task.title" class="task-card__title" :title="props.task.title" v-html="titleHtml"></div>

    <!-- Row 3: the meta line. priority · impact · labels · provenance · the
         signals that did not fit the first row. A label filters the board. -->
    <div v-if="tags.length || extraSignals.length" class="task-card__tags">
      <template v-for="t in tags" :key="t.rawTag">
        <button
          v-if="t.kind === 'label'"
          type="button"
          class="task-card__tag task-card__tag--link"
          :data-tag="t.rawTag"
          :title="`Filter board by tag: ${t.rawTag}`"
          @click="filterByTag(t.rawTag, $event)"
        >{{ t.label }}</button>
        <span v-else class="task-card__tag" :class="t.tone ? 'task-card__tag--' + t.tone : ''" :data-tag="t.rawTag">{{ t.label }}</span>
      </template>
      <span v-for="sig in extraSignals" :key="sig.label" class="task-card__tag" :title="sig.title">{{ sig.label }}</span>
    </div>

    <!-- Row 4: prompt preview (markdown) -->
    <div v-if="showPromptPreview" class="task-card__prose card-prose" v-html="promptHtml"></div>

    <!-- Row 5 (failed): error well + stop reason -->
    <template v-if="props.task.status === 'failed' && props.task.result">
      <div class="task-card__out task-card__out--err">
        <span class="task-card__out-label">Error</span>{{ errorSnippet(props.task) }}
      </div>
      <div v-if="props.task.stop_reason" class="task-card__tags">
        <span class="task-card__tag task-card__tag--err">{{ props.task.stop_reason }}</span>
      </div>
    </template>

    <!-- Row 5 (waiting): output well -->
    <div v-else-if="props.task.status === 'waiting' && props.task.result" class="task-card__out">
      <span class="task-card__out-label">Output</span>{{ waitingSnippet(props.task) }}
    </div>

    <!-- Row 5 (done/cancelled): result preview (markdown) -->
    <div v-else-if="showResultPreview" class="task-card__prose task-card__prose--result card-prose" v-html="resultHtml"></div>

    <!-- Behind-upstream: clicking Sync fires POST /api/git/sync for this task. -->
    <div v-if="showsBehind && behind.total.value > 0" class="task-card__behind" @click.stop>
      <span>{{ behind.total.value }} commit{{ behind.total.value === 1 ? '' : 's' }} behind</span>
      <button type="button" class="btn sm ghost" @click="syncFromCard">Sync</button>
    </div>

    <!-- Cost budget bar (running/waiting with a max_cost_usd). -->
    <div v-if="costBar" class="task-card__bar" :title="costBar.title">
      <div class="task-card__bar-fill" :style="{ width: costBar.pct + '%', background: costBar.color }" />
    </div>

    <!-- Row 7: turns · cost -->
    <div
      v-if="showCostMeta(props.task) && (props.task.status === 'in_progress' || props.task.status === 'waiting' || props.task.status === 'done')"
      class="task-card__foot"
    >
      <span v-if="props.task.turns > 0" :title="'Turns: ' + props.task.turns">{{ props.task.turns }} turn{{ props.task.turns === 1 ? '' : 's' }}</span>
      <span title="Total cost">{{ formatCost(props.task.usage.cost_usd) }}</span>
    </div>

    <!-- Row 8a: routine footer (replaces action buttons for routine cards) -->
    <div v-if="isRoutine" class="routine-footer" @click.stop>
      <div class="routine-footer-row">
        <span class="pill pill-brand" title="Routine schedule">routine</span>
        <span class="pill pill-neutral" :title="'Spawns ' + routineSpawnLabel + ' tasks'">{{ routineSpawnLabel }}</span>
        <span class="routine-next-run" title="Next scheduled fire">{{ routineCountdown }}</span>
      </div>
      <div class="routine-footer-row">
        <label class="routine-interval-label">
          Every
          <AppSelect
            class="routine-interval-select"
            :model-value="routineMinutes"
            :options="routineIntervalOptions"
            aria-label="Routine interval"
            @update:model-value="onRoutineIntervalChange"
          />
        </label>
        <label class="routine-enabled-label">
          <input
            type="checkbox"
            class="routine-enabled-toggle"
            :checked="routineEnabled"
            aria-label="Routine enabled"
            @change="onRoutineEnabledChange"
          />
          <span>Enabled</span>
        </label>
        <button type="button" class="btn sm ghost routine-trigger-btn" title="Spawn an instance task now" @click="onRoutineTrigger">Run now</button>
      </div>
      <div v-if="routineLastFired" class="routine-footer-row routine-last-fired">{{ routineLastFired }}</div>
    </div>

    <!-- Row 8b: actions. The forward transition is the ink button. -->
    <div v-else-if="cardActions.length" class="task-card__actions">
      <button
        v-for="a in cardActions"
        :key="a.id"
        type="button"
        :class="['btn', 'sm', a.id === primaryAction ? '' : 'ghost']"
        :title="a.title"
        :data-action="a.id"
        @click="(e) => runCardAction(a.id, e)"
      >{{ a.label }}</button>
    </div>
  </div>
</template>
