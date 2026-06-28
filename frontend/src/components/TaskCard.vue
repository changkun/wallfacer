<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import { useRouter } from 'vue-router';
import { useTaskStore } from '../stores/tasks';
import { useUiStore } from '../stores/ui';
import { api } from '../api/client';
import type { Task } from '../api/types';
import { renderMarkdown } from '../lib/markdown';
import { highlightMatch } from '../lib/highlight';
import { classifyTag, type RenderedTag } from '../lib/tagBadge';
import { cardActionsFor, CARD_ACTION_DEFS, type CardAction } from '../lib/cardActions';
import AppSelect from './AppSelect.vue';
import { dependencyBadge, failureLabel } from '../lib/cardBadges';
import { useBehindCounts } from '../composables/useBehindCounts';
import { useNow } from '../composables/useNow';
import { toRef } from 'vue';

const props = defineProps<{ task: Task; rank?: number }>();

const ROUTINE_INTERVAL_OPTIONS = [1, 5, 15, 30, 60, 180, 360, 720, 1440];
const ROUTINE_STOPPED_STATUSES = new Set(['cancelled', 'done', 'failed']);

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

onMounted(() => {
  if (uiStore.dispatchedIds.has(props.task.id)) {
    justDispatched.value = true;
    pulseHandle = setTimeout(() => {
      justDispatched.value = false;
      uiStore.consumeDispatched(props.task.id);
    }, 1500);
  }
});

onBeforeUnmount(() => {
  if (pulseHandle !== null) clearTimeout(pulseHandle);
});

const routineCountdown = computed(() => {
  const t = props.task;
  if (t.archived) return 'stopped (archived)';
  if (t.status && ROUTINE_STOPPED_STATUSES.has(t.status)) return 'stopped';
  if (!routineEnabled.value) return 'paused';
  const nextRun = t.routine_next_run;
  if (!nextRun) return 're-arming...';
  const next = new Date(nextRun).getTime();
  if (Number.isNaN(next)) return '-';
  const diffMs = next - now.value;
  if (diffMs <= 0) return 'fired just now';
  const total = Math.floor(diffMs / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return `in ${h}h ${m}m`;
  if (m > 0) return `in ${m}m ${s}s`;
  return `in ${s}s`;
});

const routineLastFired = computed(() => {
  const iso = props.task.routine_last_fired_at;
  if (!iso) return '';
  const fired = new Date(iso).getTime();
  if (Number.isNaN(fired)) return '';
  const diffMs = now.value - fired;
  if (diffMs < 0) return '';
  const sec = Math.floor(diffMs / 1000);
  if (sec < 60) return `fired ${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `fired ${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `fired ${hr}h ago`;
  return `fired ${Math.floor(hr / 24)}d ago`;
});

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
    'card-routine': task.kind === 'routine',
    'card--just-created': justDispatched.value,
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
    case 'pass': return { label: '✓ verified', cls: 'badge-test-pass', title: 'Verification passed' };
    case 'fail': return { label: '✗ verify failed', cls: 'badge-test-fail', title: 'Verification failed' };
    case 'unknown': return { label: 'no verdict', cls: 'badge-test-none', title: 'Tested — no clear verdict detected' };
    default:
      if (t.status === 'waiting') {
        return { label: 'unverified', cls: 'badge-test-none', title: 'Not yet verified' };
      }
      return null;
  }
});

function renderedTag(rawTag: string): RenderedTag {
  return classifyTag(rawTag);
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

const cardActions = computed(() =>
  cardActionsFor(props.task).map((id) => CARD_ACTION_DEFS[id]),
);

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
    case 'blocked': return 'badge-blocked';
    case 'ready': return 'badge-deps-met';
    case 'cancelled': return 'badge-dep-cancelled';
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

// Cost budget progress bar on running/waiting cards.
const costBar = computed(() => {
  const t = props.task;
  const max = t.max_cost_usd ?? 0;
  if (!(max > 0) || !(t.status === 'in_progress' || t.status === 'waiting')) return null;
  const spent = t.usage?.cost_usd ?? 0;
  const pct = Math.min(100, (spent / max) * 100);
  const color = pct >= 90 ? 'var(--red,#ef4444)' : pct >= 70 ? 'var(--yellow,#f59e0b)' : 'var(--green,#22c55e)';
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
  // Match the actual card class — `card` is the root (see cardClasses()),
  // not `task-card`. A wrong selector here silently breaks card-to-card
  // keyboard nav (the focusSibling never finds any peers).
  const all = Array.from(document.querySelectorAll<HTMLElement>('.card[tabindex="0"]'));
  const idx = all.indexOf(root);
  if (idx < 0 || all.length === 0) return;
  let nextIdx = idx;
  if (direction === 'next') nextIdx = Math.min(all.length - 1, idx + 1);
  else if (direction === 'prev') nextIdx = Math.max(0, idx - 1);
  else {
    // Left/Right: find the nearest card whose column differs and whose
    // vertical centre is closest to ours.
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
    <!-- Row 1: status badge + meta-right (harness, timeout, time) -->
    <div class="flex items-center mb-1 card-row1">
      <div class="flex items-center gap-1.5 card-badge-row">
        <span v-if="props.rank" class="card-rank" title="Backlog position">#{{ props.rank }}</span>
        <span :class="['badge', badgeClass(props.task)]">{{ statusLabel(props.task) }}</span>
        <span v-if="showSpinner(props.task)" class="spinner"></span>
        <span
          v-if="failureBadge"
          class="badge badge-failure-category"
          :title="'Failure reason: ' + props.task.failure_category"
        >{{ failureBadge }}</span>
        <span
          v-if="depBadge"
          :class="['badge', depBadgeClass]"
          :title="depBadgeTitle"
        >{{ depBadgeText }}</span>
        <span
          v-if="scheduledLabel"
          class="badge badge-scheduled"
          title="Scheduled start"
        >{{ scheduledLabel }}</span>
        <span
          v-if="testBadge"
          :class="['badge', testBadge.cls]"
          :title="testBadge.title"
        >{{ testBadge.label }}</span>
      </div>
      <div class="flex items-center gap-1.5 card-meta-right">
        <span
          class="text-xs text-v-muted"
          :title="'Harness: ' + sandboxLabel(props.task)"
        >{{ sandboxLabel(props.task) }}</span>
        <span class="text-xs text-v-muted" title="Timeout">{{ formatTimeout(props.task.timeout) }}</span>
        <span class="text-xs text-v-muted" :title="'Created ' + props.task.created_at">{{ timeAgo(props.task.created_at) }}</span>
      </div>
    </div>

    <!-- Row 2: title -->
    <div v-if="props.task.title" class="card-title" :title="props.task.title" v-html="titleHtml"></div>

    <!-- Row 3: tags (priority:*/impact:* get dedicated badges).
         Click a tag to filter the board to that tag. -->
    <div v-if="props.task.tags?.length" class="tag-chip-row">
      <button
        v-for="tag in props.task.tags"
        :key="tag"
        type="button"
        :class="renderedTag(tag).cls"
        :data-tag="tag"
        :style="renderedTag(tag).styled ? tagStyle(tag) : ''"
        :title="`Filter board by tag: ${tag}`"
        @click="filterByTag(tag, $event)"
      >{{ renderedTag(tag).label }}</button>
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

    <!-- Behind-upstream banner (waiting/failed only). Mirrors the legacy
         applyDiffToCard() warning; clicking Sync fires POST /api/git/sync
         for this task without opening the card. -->
    <div
      v-if="showsBehind && behind.total.value > 0"
      class="diff-behind-warning"
      @click.stop
    >
      <span>⚠ {{ behind.total.value }} commit{{ behind.total.value === 1 ? '' : 's' }} behind</span>
      <button type="button" class="diff-sync-btn" @click="syncFromCard">Sync</button>
    </div>

    <!-- Cost budget progress bar (running/waiting with a max_cost_usd). -->
    <div
      v-if="costBar"
      class="card-cost-bar"
      :title="costBar.title"
      style="margin-top:4px;height:3px;border-radius:2px;background:var(--border);overflow:hidden;"
    >
      <div :style="{ height:'100%', width: costBar.pct + '%', background: costBar.color, transition:'width 0.3s' }" />
    </div>

    <!-- Row 7: meta footer (turns, cost, session id) -->
    <div
      v-if="showCostMeta(props.task) && (props.task.status === 'in_progress' || props.task.status === 'waiting' || props.task.status === 'done')"
      class="card-meta"
      style="display:flex;align-items:center;gap:8px;margin-top:6px;font-size:10px;color:var(--ink-4);"
    >
      <span v-if="props.task.turns > 0" class="card-meta-time" :title="'Turns: ' + props.task.turns">{{ props.task.turns }} turn{{ props.task.turns === 1 ? '' : 's' }}</span>
      <span class="card-meta-cost" title="Total cost">{{ formatCost(props.task.usage.cost_usd) }}</span>
    </div>

    <!-- Row 8a: routine footer (replaces action buttons for routine cards) -->
    <div v-if="isRoutine" class="routine-footer" @click.stop>
      <div class="routine-footer-row">
        <span class="badge badge-routine" title="Routine schedule">routine</span>
        <span class="badge badge-routine-spawn" :title="'Spawns ' + routineSpawnLabel + ' tasks'">{{ routineSpawnLabel }}</span>
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
        <button
          type="button"
          class="routine-trigger-btn"
          title="Spawn an instance task now"
          @click="onRoutineTrigger"
        >Run now</button>
      </div>
      <div v-if="routineLastFired" class="routine-footer-row routine-last-fired">{{ routineLastFired }}</div>
    </div>

    <!-- Row 8b: action buttons (non-routine) -->
    <div v-else-if="cardActions.length" class="card-actions">
      <button
        v-for="a in cardActions"
        :key="a.id"
        type="button"
        :class="['card-action-btn', a.cls]"
        :title="a.title"
        @click="(e) => runCardAction(a.id, e)"
      >{{ a.icon }} {{ a.label }}</button>
    </div>
  </div>
</template>
