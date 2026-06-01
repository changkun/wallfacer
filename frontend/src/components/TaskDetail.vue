<script setup lang="ts">
import { ref, computed, nextTick, watch, onMounted, onUnmounted } from 'vue';
import { api } from '../api/client';
import { useTaskActivity } from '../composables/useTaskActivity';
import { parseDiffFiles, type DiffFile } from '../lib/diff';
import type { ActivityRow } from '../lib/prettyNdjson';
import type { Task } from '../api/types';
import { useMentions } from '../composables/useMentions';
import { useDialogStore } from '../stores/dialog';
import { useToastStore } from '../stores/toast';
import { useTaskStore } from '../stores/tasks';
import { useRouter } from 'vue-router';
import SpanFlamegraph from './SpanFlamegraph.vue';
import type { SpanResult } from '../lib/flamegraph';
import { detectResultType } from '../lib/resultType';
// Re-imported as a local binding so the template can call renderMarkdown()
// directly inside the Results tab.
import { renderMarkdown as renderResultMarkdown } from '../lib/markdown';
import { ansiToHtml } from '../lib/ansi';
import { useFocusTrap } from '../composables/useFocusTrap';

const props = defineProps<{ task: Task }>();
const emit = defineEmits<{ close: [] }>();

const taskStore = useTaskStore();
const detailRouter = useRouter();

// "Blocked by" dependency list — resolves depends_on against the live task
// store, mirroring modal-core's dependency section. Each row is clickable
// to open the blocking task; a "Waiting on X of Y" summary shows how many
// are still unsatisfied (not done/cancelled).
interface DepRow { id: string; label: string; status: string; satisfied: boolean }
const blockedBy = computed<DepRow[]>(() => {
  const ids = props.task.depends_on ?? [];
  const byId = new Map(taskStore.tasks.map((t) => [t.id, t]));
  return ids.map((id) => {
    const dep = byId.get(id);
    if (!dep) return { id, label: id.slice(0, 8) + '…', status: 'missing', satisfied: false };
    const label = dep.title || (dep.prompt.length > 40 ? dep.prompt.slice(0, 40) + '…' : dep.prompt) || id.slice(0, 8);
    const satisfied = dep.status === 'done' || dep.status === 'cancelled';
    return { id, label, status: dep.status, satisfied };
  });
});
const blockedByUnmet = computed(() => blockedBy.value.filter((d) => !d.satisfied).length);
function openDep(id: string) {
  detailRouter.push({ path: '/', query: { task: id } });
}

// Modal focus trap — Tab cycles inside the dialog only, focus restores
// to the element that triggered the open on close. Must be declared
// AFTER defineProps so the `() => !!props.task` getter doesn't access
// `props` during its initial synchronous watch (TDZ on `props` —
// useFocusTrap's `watch(immediate: true)` evaluates the predicate
// before the surrounding setup function finishes declaring locals).
const modalRoot = ref<HTMLElement | null>(null);
useFocusTrap(modalRoot, () => !!props.task);
const dialog = useDialogStore();
const toast = useToastStore();

type MainTab = 'spec' | 'activity' | 'changes' | 'results' | 'events' | 'timeline';
const mainTab = ref<MainTab>('spec');

// --- Changes (diff) tab ---
const diffFiles = ref<DiffFile[]>([]);
const diffLoading = ref(false);
const diffError = ref('');
const diffFetched = ref(false);
const behindCounts = ref<Record<string, number>>({});
const behindTotal = computed(() =>
  Object.values(behindCounts.value).reduce((a, b) => a + (b || 0), 0),
);

async function fetchDiff() {
  if (diffLoading.value) return;
  diffLoading.value = true;
  diffError.value = '';
  try {
    const data = await api<{ behind_counts?: Record<string, number>; diff?: string }>(
      'GET',
      `/api/tasks/${props.task.id}/diff`,
    );
    diffFiles.value = parseDiffFiles(data?.diff || '');
    behindCounts.value = data?.behind_counts || {};
    diffFetched.value = true;
  } catch (e) {
    diffError.value = e instanceof Error ? e.message : String(e);
  } finally {
    diffLoading.value = false;
  }
}

// --- Results (multi-turn) tab ---
//
// One entry per "output" event with a non-empty result text. Implementation
// turns vs test turns are separated using task.test_run_start_turn: turns
// before that index are implementation, turns >= are test. Newest-first
// to match ui/js/modal-results.js (line 603).

interface ResultEntry {
  turn: number;      // 1-based chronological turn number
  text: string;
  type: 'plan' | 'result';
  showRaw: boolean;
}
const implResults = ref<ResultEntry[]>([]);
const testResults = ref<ResultEntry[]>([]);
const resultsLoading = ref(false);
const resultsError = ref('');
const resultsFetched = ref(false);

async function fetchResults() {
  if (!props.task) return;
  resultsLoading.value = true;
  resultsError.value = '';
  try {
    const data = await api<{ events?: { event_type: string; data?: { result?: string } }[] } | { event_type: string; data?: { result?: string } }[]>(
      'GET',
      `/api/tasks/${props.task.id}/events?type=output`,
    );
    const events = Array.isArray(data) ? data : (data?.events ?? []);
    const outputs = events
      .filter((e) => e.event_type === 'output' && typeof e.data?.result === 'string' && e.data.result.length > 0)
      .map((e) => e.data!.result as string);
    // The legacy UI split outputs at task.test_run_start_turn into
    // implementation vs test runs; that field is no longer maintained by
    // the backend so we surface every turn as an implementation entry.
    // (When the field is reintroduced, slice the array here.)
    const impl = outputs;
    const tests: string[] = [];
    const split = 0;
    implResults.value = impl.map((text, i) => ({
      turn: i + 1,
      text,
      type: detectResultType(text),
      showRaw: false,
    })).reverse();
    testResults.value = tests.map((text, i) => ({
      turn: i + 1 + (split > 0 ? split : impl.length),
      text,
      type: detectResultType(text),
      showRaw: false,
    })).reverse();
    resultsFetched.value = true;
  } catch (e) {
    resultsError.value = e instanceof Error ? e.message : String(e);
  } finally {
    resultsLoading.value = false;
  }
}

function copyResult(entry: ResultEntry) {
  void navigator.clipboard.writeText(entry.text);
  toast.push('Copied to clipboard', { kind: 'success', timeout: 2000 });
}

// --- Timeline (span flamegraph) tab ---

const spans = ref<SpanResult[]>([]);
const spansLoading = ref(false);
const spansFetched = ref(false);
const spansError = ref('');

async function fetchSpans() {
  if (!props.task) return;
  spansLoading.value = true;
  spansError.value = '';
  try {
    const data = await api<{ spans?: SpanResult[] }>('GET', `/api/tasks/${props.task.id}/spans`);
    spans.value = data?.spans ?? [];
    spansFetched.value = true;
  } catch (e) {
    spansError.value = e instanceof Error ? e.message : String(e);
  } finally {
    spansLoading.value = false;
  }
}

// --- Events tab: the actual event timeline (state_change / output /
// feedback / error / system), not just usage stats. Mirrors the legacy
// modal-core event list. ---
interface TaskEvent {
  id: number;
  event_type: string;
  data?: Record<string, unknown>;
  created_at: string;
}
const events = ref<TaskEvent[]>([]);
const eventsLoading = ref(false);
const eventsFetched = ref(false);

async function fetchEvents() {
  if (!props.task) return;
  eventsLoading.value = true;
  try {
    const data = await api<TaskEvent[] | { events?: TaskEvent[] }>('GET', `/api/tasks/${props.task.id}/events`);
    events.value = Array.isArray(data) ? data : (data?.events ?? []);
    eventsFetched.value = true;
  } catch {
    events.value = [];
  } finally {
    eventsLoading.value = false;
  }
}

// One-line summary per event, by type. Mirrors the legacy _renderEventRow.
function eventSummary(e: TaskEvent): string {
  const d = e.data ?? {};
  switch (e.event_type) {
    case 'state_change': return `${d.from ?? '?'} → ${d.to ?? d.status ?? '?'}`;
    case 'output': return typeof d.result === 'string' ? d.result.slice(0, 100) : 'output';
    case 'feedback': return typeof d.text === 'string' ? d.text.slice(0, 100) : 'feedback';
    case 'error': return typeof d.error === 'string' ? d.error.slice(0, 120) : (typeof d.message === 'string' ? d.message.slice(0, 120) : 'error');
    case 'system': return typeof d.kind === 'string' ? d.kind : 'system';
    default: return e.event_type;
  }
}
const visibleEvents = computed(() =>
  // span_start/span_end belong to the Timeline tab, not the event list.
  events.value.filter((e) => e.event_type !== 'span_start' && e.event_type !== 'span_end'),
);

// Per-sub-agent usage breakdown (implementation/test/refinement/oversight/…).
const USAGE_ACTIVITY_LABELS: Record<string, string> = {
  implementation: 'Implementation',
  test: 'Test',
  refinement: 'Refinement',
  oversight: 'Oversight',
  'oversight-test': 'Oversight (test)',
  title: 'Title',
  commit_message: 'Commit msg',
  ideation: 'Ideation',
};
const usageBreakdown = computed(() => {
  const bd = props.task.usage_breakdown ?? {};
  return Object.entries(bd)
    .filter(([, u]) => u && (u.input_tokens || u.output_tokens || u.cost_usd))
    .map(([activity, u]) => ({
      label: USAGE_ACTIVITY_LABELS[activity] ?? activity,
      input: u.input_tokens ?? 0,
      output: u.output_tokens ?? 0,
      cost: u.cost_usd ?? 0,
    }));
});

// Retry history (past retired attempts) and prompt history (prior prompts).
const retryHistory = computed(() => props.task.retry_history ?? []);
const promptHistory = computed(() => props.task.prompt_history ?? []);

watch(mainTab, (t) => {
  if (t === 'changes' && !diffFetched.value) fetchDiff();
  if (t === 'activity') fetchOversight();
  if (t === 'results' && !resultsFetched.value) fetchResults();
  if (t === 'timeline' && !spansFetched.value) fetchSpans();
  if (t === 'events' && !eventsFetched.value) fetchEvents();
});

// Refetch per-tab data when navigating to a different task while a data
// tab stays selected (e.g. via deep-link or sidebar nav).
watch(
  () => props.task?.id,
  () => {
    spansFetched.value = false; spans.value = [];
    resultsFetched.value = false; implResults.value = []; testResults.value = [];
    eventsFetched.value = false; events.value = [];
    if (mainTab.value === 'timeline') fetchSpans();
    if (mainTab.value === 'results') fetchResults();
    if (mainTab.value === 'events') fetchEvents();
  },
);

function lineClass(kind: string): string {
  return kind === 'ctx' ? '' : 'diff-' + kind;
}
function showWorkspaceHeader(i: number): boolean {
  const f = diffFiles.value[i];
  if (!f.workspace) return false;
  return i === 0 || diffFiles.value[i - 1].workspace !== f.workspace;
}

const feedback = ref('');
const feedbackRef = ref<HTMLTextAreaElement | null>(null);
const fbMentions = useMentions({ setValue: (v) => { feedback.value = v; }, priorityPrefix: 'spec/' });
const submittingFeedback = ref(false);
const logContainer = ref<HTMLElement | null>(null);

// Stream output for every task (the endpoint replays saved turn outputs for
// finished tasks and live output for running ones).
const streamTaskId = computed(() => props.task.id || null);
const { raw: rawOutput, activity, streaming } = useTaskActivity(streamTaskId);

// Activity search + truncation. Mirrors ui/js/modal-logs.js (line 142
// search filter + line 117 MAX_LOG_LINES cap). Filter is case-insensitive
// across label/summary/detail. Cap protects the DOM from runaway agents.
const activitySearch = ref('');
const ACTIVITY_MAX_ROWS = 5000;
const visibleActivity = computed(() => {
  const q = activitySearch.value.trim().toLowerCase();
  const filtered = q
    ? activity.value.filter((row) => {
        const hay = `${row.label} ${row.summary ?? ''} ${row.detail ?? ''}`.toLowerCase();
        return hay.includes(q);
      })
    : activity.value;
  if (filtered.length > ACTIVITY_MAX_ROWS) {
    return filtered.slice(filtered.length - ACTIVITY_MAX_ROWS);
  }
  return filtered;
});
const activityTruncated = computed(() => {
  const q = activitySearch.value.trim().toLowerCase();
  const len = q ? visibleActivity.value.length : activity.value.length;
  return activity.value.length > ACTIVITY_MAX_ROWS && len === ACTIVITY_MAX_ROWS;
});

// Server-side 8MB turn-output truncation: the backend injects a
// truncation_notice sentinel into the NDJSON stream (see store SaveTurnOutput).
// Mirrors ui/js/modal-logs.js's server truncation banner.
const serverTruncated = computed(() => rawOutput.value.includes('"subtype":"truncation_notice"'));
const logDownloadUrl = computed(() => `/api/tasks/${props.task.id}/logs`);

interface OversightPhase {
  title: string;
  summary: string;
  tools_used?: string[];
  actions?: string[];
}
const oversightStatus = ref('');
const oversightPhases = ref<OversightPhase[]>([]);
const oversightError = ref('');
let oversightTimer: ReturnType<typeof setTimeout> | null = null;
let oversightTaskId = '';

function stopOversightPolling() {
  if (oversightTimer) { clearTimeout(oversightTimer); oversightTimer = null; }
}

async function fetchOversight() {
  // Reset state if the user switched to a different task while keeping the
  // Activity tab open.
  if (oversightTaskId !== props.task.id) {
    stopOversightPolling();
    oversightTaskId = props.task.id;
    oversightStatus.value = '';
    oversightPhases.value = [];
    oversightError.value = '';
  }
  try {
    const data = await api<{ status?: string; phases?: OversightPhase[]; error?: string }>(
      'GET',
      `/api/tasks/${props.task.id}/oversight`,
    );
    oversightStatus.value = data?.status ?? '';
    oversightPhases.value = data?.phases ?? [];
    oversightError.value = data?.error ?? '';
    // The server emits status === 'generating' (or no status yet) while the
    // summary is being produced. Re-poll every 3 s until it transitions —
    // mirrors ui/js/modal-oversight.js line 73. Stop on ready / failed /
    // error or when the tab unmounts.
    if (data?.status === 'generating' || data?.status === 'pending') {
      oversightTimer = setTimeout(() => {
        if (mainTab.value === 'activity' && oversightTaskId === props.task.id) {
          void fetchOversight();
        }
      }, 3000);
    }
  } catch {
    oversightStatus.value = 'error';
    oversightError.value = '';
  }
}

function activityIcon(kind: ActivityRow['kind']): string {
  switch (kind) {
    case 'tool': return '▶';
    case 'tool_result': return '✓';
    case 'thinking': return '🧠';
    default: return '·';
  }
}

watch(activity, async () => {
  await nextTick();
  if (logContainer.value) {
    logContainer.value.scrollTop = logContainer.value.scrollHeight;
  }
}, { deep: true });

watch(() => props.task.status, (s) => {
  if (s === 'in_progress' || s === 'committing') mainTab.value = 'activity';
});

const costDisplay = computed(() => {
  const usd = props.task.usage?.cost_usd ?? 0;
  if (usd === 0) return 'no cost';
  if (usd < 0.01) return '<$0.01';
  return '$' + usd.toFixed(2);
});

const tokenCount = (n: number) => (n || 0).toLocaleString();

// Status → badge class, for retry-history rows (matches the board badges).
function badgeClassFor(status: string): string {
  return `badge-${status}`;
}

function timeStr(iso: string): string {
  if (!iso) return '—';
  return new Date(iso).toLocaleString();
}

function relativeTime(iso: string): string {
  if (!iso) return '';
  const now = Date.now();
  const then = new Date(iso).getTime();
  const diff = Math.floor((now - then) / 1000);
  if (diff < 60) return diff + 's ago';
  if (diff < 3600) return Math.floor(diff / 60) + 'm ago';
  if (diff < 86400) return Math.floor(diff / 3600) + 'h ago';
  return Math.floor(diff / 86400) + 'd ago';
}

const elapsedDisplay = computed(() => relativeTime(props.task.updated_at));

async function startTask() {
  await api('PATCH', `/api/tasks/${props.task.id}`, { status: 'in_progress' });
}
// `cancelling` tracks the in-flight POST so the button shows
// "Shutting down…" and stays disabled until the request returns or
// the task transitions out of cancelling state. Mirrors the legacy
// pendingCancelTaskIds Set.
const cancelling = ref(false);
async function cancelTask() {
  const ok = await dialog.confirm({
    title: 'Cancel task',
    message: 'Cancel this task? Running work is stopped and uncommitted worktree changes are discarded.',
    confirmLabel: 'Cancel task',
    cancelLabel: 'Keep',
    danger: true,
  });
  if (!ok) return;
  cancelling.value = true;
  try {
    await api('POST', `/api/tasks/${props.task.id}/cancel`);
  } finally {
    // Hold the visual a moment so the user sees the shutdown phase even
    // if the API replies instantly — the actual container kill is async.
    setTimeout(() => { cancelling.value = false; }, 1500);
  }
}
async function retryTask() {
  await api('PATCH', `/api/tasks/${props.task.id}`, { status: 'backlog' });
}
async function completeTask() {
  await api('POST', `/api/tasks/${props.task.id}/done`);
}
async function resumeTask() {
  await api('POST', `/api/tasks/${props.task.id}/resume`);
}
async function testTask() {
  // Optional acceptance criteria — when set, the test agent focuses on
  // the exact checks the user described. Empty / cancelled = generic run.
  const criteria = await dialog.prompt({
    title: 'Test verification',
    message: 'Acceptance criteria (optional). Leave blank for a generic verification run.',
    placeholder: 'e.g. all unit tests pass; CHANGELOG updated',
  });
  if (criteria === null) return;
  const body = criteria.trim() ? { criteria: criteria.trim() } : undefined;
  await api('POST', `/api/tasks/${props.task.id}/test`, body);
}
async function syncTask() {
  await api('POST', `/api/tasks/${props.task.id}/sync`);
}

// Backlog editing — surfaces a few common knobs (timeout, model, budgets,
// tags) as an inline editor that PATCHes the task. Lets the user adjust
// settings after creation without having to retype the prompt. Mirrors
// the legacy modal-core editable settings (timeout/tags/deps/budget).
const editingBacklog = ref(false);
const editTimeout = ref<number | null>(null);
const editModel = ref('');
const editTags = ref('');
const editMaxCost = ref<number | null>(null);
const editMaxTokens = ref<number | null>(null);
const editSaving = ref(false);

function openBacklogEdit() {
  const t = props.task;
  editTimeout.value = t.timeout > 0 ? Math.round(t.timeout / 60) : null;
  editModel.value = t.model ?? '';
  editTags.value = (t.tags ?? []).join(', ');
  editMaxCost.value = t.max_cost_usd && t.max_cost_usd > 0 ? t.max_cost_usd : null;
  editMaxTokens.value = t.max_input_tokens && t.max_input_tokens > 0 ? t.max_input_tokens : null;
  editingBacklog.value = true;
}

async function saveBacklogEdit() {
  editSaving.value = true;
  try {
    const patch: Record<string, unknown> = {};
    if (editTimeout.value !== null && editTimeout.value > 0) patch.timeout = editTimeout.value * 60;
    if (editModel.value.trim() !== (props.task.model ?? '').trim()) patch.model = editModel.value.trim();
    const parsedTags = editTags.value.split(',').map((t) => t.trim()).filter(Boolean);
    if (JSON.stringify(parsedTags) !== JSON.stringify(props.task.tags ?? [])) patch.tags = parsedTags;
    if ((editMaxCost.value ?? 0) !== (props.task.max_cost_usd ?? 0)) patch.max_cost_usd = editMaxCost.value ?? 0;
    if ((editMaxTokens.value ?? 0) !== (props.task.max_input_tokens ?? 0)) patch.max_input_tokens = editMaxTokens.value ?? 0;
    if (Object.keys(patch).length === 0) { editingBacklog.value = false; return; }
    await api('PATCH', `/api/tasks/${props.task.id}`, patch);
    toast.push('Settings updated', { kind: 'success' });
    editingBacklog.value = false;
  } catch (e) {
    toast.push(`Save failed: ${e instanceof Error ? e.message : String(e)}`, { kind: 'error' });
  } finally {
    editSaving.value = false;
  }
}

// Raise the cost / token budget on a task that hit a guardrail. Two
// sequential prompts mirror ui/js/tasks.js:912. PATCH the task with the
// new caps; an empty / 0 entry means "unlimited".
async function raiseBudget() {
  const cur = props.task;
  const newCost = await dialog.prompt({
    title: 'Raise cost limit',
    message: `New cost limit in USD (0 = unlimited). Current: ${cur.max_cost_usd && cur.max_cost_usd > 0 ? '$' + cur.max_cost_usd.toFixed(2) : 'none'}.`,
    initial: cur.max_cost_usd && cur.max_cost_usd > 0 ? String(cur.max_cost_usd) : '',
    placeholder: '10',
  });
  if (newCost === null) return;
  const newTokens = await dialog.prompt({
    title: 'Raise token limit',
    message: `New input-token limit (0 = unlimited). Current: ${cur.max_input_tokens && cur.max_input_tokens > 0 ? cur.max_input_tokens.toLocaleString() : 'none'}.`,
    initial: cur.max_input_tokens && cur.max_input_tokens > 0 ? String(cur.max_input_tokens) : '',
    placeholder: '100000',
  });
  if (newTokens === null) return;
  try {
    await api('PATCH', `/api/tasks/${cur.id}`, {
      max_cost_usd: parseFloat(newCost) || 0,
      max_input_tokens: parseInt(newTokens, 10) || 0,
    });
    toast.push('Budget updated', { kind: 'success' });
  } catch (e) {
    toast.push(`Failed to update budget: ${e instanceof Error ? e.message : String(e)}`, { kind: 'error' });
  }
}

// Surface the Raise Budget button when the task was paused for budget
// reasons. Two conditions cover both shapes the server emits.
const budgetExceeded = computed(() =>
  props.task.failure_category === 'budget_exceeded' ||
  (props.task.stop_reason ?? '').toLowerCase().includes('budget'),
);
async function archiveTask() {
  const id = props.task.id;
  await api('POST', `/api/tasks/${id}/archive`);
  toast.pushWithAction('Task archived', 'Undo', () => {
    api('POST', `/api/tasks/${id}/unarchive`).catch((e) => console.error('unarchive:', e));
  }, { kind: 'success' });
}
async function unarchiveTask() {
  await api('POST', `/api/tasks/${props.task.id}/unarchive`);
}
async function deleteTask() {
  const ok = await dialog.confirm({
    title: 'Delete task',
    message: 'Delete this task? It is soft-deleted (recoverable within the retention window) but removed from the board.',
    confirmLabel: 'Delete',
    cancelLabel: 'Keep',
    danger: true,
  });
  if (!ok) return;
  await api('DELETE', `/api/tasks/${props.task.id}`);
  emit('close');
}
async function submitFeedback() {
  const text = feedback.value.trim();
  if (!text || submittingFeedback.value) return;
  submittingFeedback.value = true;
  try {
    await api('POST', `/api/tasks/${props.task.id}/feedback`, { feedback: text });
    feedback.value = '';
  } catch (e) {
    console.error('feedback:', e);
  } finally {
    submittingFeedback.value = false;
  }
}

function onBackdrop(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('modal-overlay')) emit('close');
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close');
}

onMounted(() => {
  document.addEventListener('keydown', onKeydown);
  if (props.task.status === 'in_progress' || props.task.status === 'committing') {
    mainTab.value = 'activity';
  }
});
onUnmounted(() => {
  document.removeEventListener('keydown', onKeydown);
  stopOversightPolling();
});

const status = computed(() => props.task.status);
const isBacklog = computed(() => status.value === 'backlog');
const isWaiting = computed(() => status.value === 'waiting');
const isInProgress = computed(() => status.value === 'in_progress' || status.value === 'committing');
const isFailed = computed(() => status.value === 'failed');
const isDone = computed(() => status.value === 'done');
const isCancelled = computed(() => status.value === 'cancelled');
const isArchived = computed(() => !!props.task.archived);
</script>

<template>
  <div
    class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4"
    @click="onBackdrop"
  >
    <div id="modal" ref="modalRoot" class="modal-card modal-wide" role="dialog" aria-modal="true" :data-main-tab="mainTab">
      <div class="p-6">
        <!-- Header row: badge / id / time / close -->
        <div class="flex items-start justify-between mb-4">
          <div class="flex items-center gap-3">
            <span class="badge" :class="'badge-' + status">{{ status }}</span>
            <span v-if="task.sandbox" class="badge badge-priority">{{ task.sandbox }}</span>
            <span class="text-xs text-v-muted">{{ relativeTime(task.updated_at) }}</span>
            <span class="text-xs text-v-muted font-mono" title="Task ID">{{ task.id.slice(0, 8) }}</span>
          </div>
          <button
            type="button"
            class="modal-close-btn"
            aria-label="Close"
            @click="emit('close')"
          >&times;</button>
        </div>

        <h2 v-if="task.title" class="modal-title">{{ task.title }}</h2>

        <div id="modal-body">
          <!-- Main tabs -->
          <div id="main-tabs" class="main-tabs" role="tablist" aria-label="Task detail sections">
            <button
              type="button"
              class="main-tab"
              :class="{ active: mainTab === 'spec' }"
              role="tab"
              :aria-selected="mainTab === 'spec'"
              aria-controls="modal-row"
              @click="mainTab = 'spec'"
            >Spec</button>
            <button
              type="button"
              class="main-tab"
              :class="{ active: mainTab === 'activity' }"
              role="tab"
              :aria-selected="mainTab === 'activity'"
              aria-controls="modal-row"
              @click="mainTab = 'activity'"
            >
              Activity
              <span v-if="streaming" class="pulse-dot" />
            </button>
            <button
              type="button"
              class="main-tab"
              :class="{ active: mainTab === 'changes' }"
              role="tab"
              :aria-selected="mainTab === 'changes'"
              aria-controls="modal-row"
              @click="mainTab = 'changes'"
            >Changes</button>
            <button
              type="button"
              class="main-tab"
              :class="{ active: mainTab === 'results' }"
              role="tab"
              :aria-selected="mainTab === 'results'"
              aria-controls="modal-row"
              @click="mainTab = 'results'"
            >Results</button>
            <button
              type="button"
              class="main-tab"
              :class="{ active: mainTab === 'events' }"
              role="tab"
              :aria-selected="mainTab === 'events'"
              aria-controls="modal-row"
              @click="mainTab = 'events'"
            >Events</button>
            <button
              type="button"
              class="main-tab"
              :class="{ active: mainTab === 'timeline' }"
              role="tab"
              :aria-selected="mainTab === 'timeline'"
              aria-controls="modal-row"
              @click="mainTab = 'timeline'"
            >Timeline</button>
          </div>

          <div id="modal-row" role="tabpanel" :aria-labelledby="`main-tab-${mainTab}`">
            <!-- Main pane -->
            <div id="modal-main-pane">
              <div id="modal-main-content">
                <!-- SPEC tab -->
                <div data-main-tab-section="spec">
                  <h3 class="section-title">Spec</h3>
                  <pre class="code-block mb-4">{{ task.prompt }}</pre>

                  <template v-if="task.result">
                    <h3 class="section-title">Result</h3>
                    <pre class="code-block mb-4">{{ task.result }}</pre>
                  </template>

                  <div v-if="isWaiting" class="mb-4">
                    <h3 class="section-title">Provide Feedback</h3>
                    <div class="fb-wrap">
                      <textarea
                        ref="feedbackRef"
                        v-model="feedback"
                        rows="3"
                        placeholder="Type your response… (@ to mention files)"
                        class="field"
                        @input="(e) => fbMentions.onInput(e.target as HTMLTextAreaElement)"
                        @keydown="(e) => fbMentions.onKeydown(e, e.target as HTMLTextAreaElement)"
                      />
                      <ul v-if="fbMentions.open.value" class="fb-mentions" role="listbox">
                        <li
                          v-for="(file, i) in fbMentions.items.value"
                          :key="file"
                          class="fb-mention"
                          :class="{ active: i === fbMentions.activeIndex.value }"
                          role="option"
                          @mousedown.prevent="fbMentions.choose(feedbackRef!, file)"
                          @mouseenter="fbMentions.activeIndex.value = i"
                        >{{ file }}</li>
                      </ul>
                    </div>
                    <div class="flex items-center gap-2 mt-2">
                      <button
                        type="button"
                        class="btn btn-yellow"
                        :disabled="!feedback.trim() || submittingFeedback"
                        @click="submitFeedback"
                      >
                        {{ submittingFeedback ? 'Sending…' : 'Submit Feedback' }}
                      </button>
                    </div>
                  </div>
                </div>

                <!-- ACTIVITY tab -->
                <div data-main-tab-section="activity">
                  <section class="activity-agent">
                    <div class="activity-block activity-block--oversight">
                      <div class="activity-block__label-row">
                        <span class="activity-block__label">Activity</span>
                        <span v-if="streaming" class="text-xs text-v-muted">streaming…</span>
                      </div>

                      <!-- Oversight summary (phase-by-phase). -->
                      <div v-if="oversightPhases.length" class="ta-oversight">
                        <div class="ta-oversight__label">Oversight summary</div>
                        <div v-for="(ph, pi) in oversightPhases" :key="pi" class="ta-oversight__phase">
                          <div class="ta-oversight__title">{{ ph.title }}</div>
                          <div class="ta-oversight__summary">{{ ph.summary }}</div>
                          <div v-if="ph.tools_used?.length" class="ta-oversight__tools">
                            <span v-for="t in ph.tools_used" :key="t" class="ta-oversight__tool">{{ t }}</span>
                          </div>
                        </div>
                      </div>
                      <div v-else-if="oversightStatus === 'pending'" class="text-xs text-v-muted">Oversight summary not yet generated.</div>
                      <div v-else-if="oversightStatus === 'generating'" class="text-xs text-v-muted">Generating oversight summary…</div>
                      <div v-else-if="oversightStatus === 'failed'" class="text-xs" style="color: var(--err, #c0392b);">Oversight generation failed{{ oversightError ? `: ${oversightError}` : '' }}</div>

                      <!-- Pretty activity rows (thinking / tool calls / results / text). -->
                      <div v-if="activity.length">
                        <div class="ta-activity-search">
                          <input
                            v-model="activitySearch"
                            type="search"
                            class="ta-activity-search__input"
                            placeholder="Filter activity…"
                            aria-label="Filter activity rows"
                          />
                          <span v-if="activitySearch" class="ta-activity-search__count">
                            {{ visibleActivity.length }} / {{ activity.length }}
                          </span>
                        </div>
                        <div v-if="serverTruncated" class="ta-activity-truncated ta-activity-truncated--warn">
                          ⚠ Turn output was truncated at the server (8&nbsp;MB limit). Some tool calls may be missing.
                          <a :href="logDownloadUrl" target="_blank" rel="noopener">Download full log</a>
                        </div>
                        <div
                          v-if="activityTruncated"
                          class="ta-activity-truncated"
                          :title="`Showing the last ${ACTIVITY_MAX_ROWS} rows; load the full log via /api/tasks/{id}/logs for the complete trace.`"
                        >
                          Showing last {{ ACTIVITY_MAX_ROWS.toLocaleString() }} rows.
                          <a :href="logDownloadUrl" target="_blank" rel="noopener">Download full log</a>
                        </div>
                        <div class="ta-activity-log">
                          <div
                            v-for="(row, i) in visibleActivity"
                            :key="i"
                            class="ta-activity-row"
                            :class="'ta-activity-row--' + row.kind"
                          >
                            <span class="ta-activity-icon" aria-hidden="true">{{ activityIcon(row.kind) }}</span>
                            <span class="ta-activity-label">{{ row.label }}</span>
                            <span v-if="row.summary" class="ta-activity-summary">{{ row.summary }}</span>
                            <details v-if="row.detail" class="ta-activity-detail" :open="row.defaultOpen">
                              <summary>{{ row.detailLabel || 'details' }}</summary>
                              <pre>{{ row.detail }}</pre>
                            </details>
                          </div>
                        </div>
                      </div>

                      <!-- Fallback: raw output when nothing parsed into activity.
                           Carriage returns are collapsed and ANSI escapes are
                           coloured via lib/ansi so spinners + warning lines
                           render the way they do in a terminal. -->
                      <div v-else class="activity-oversight-box" id="modal-logs-section">
                        <pre v-if="!rawOutput" ref="logContainer" class="logs-block"><span class="cc-result-empty">{{ streaming ? 'Connecting…' : 'No output' }}</span></pre>
                        <!-- eslint-disable-next-line vue/no-v-html — ansiToHtml escapes its input -->
                        <pre v-else ref="logContainer" class="logs-block" v-html="ansiToHtml(rawOutput)"></pre>
                      </div>
                    </div>
                  </section>
                </div>

                <!-- CHANGES (diff) tab -->
                <div data-main-tab-section="changes">
                  <div v-if="diffLoading" class="text-xs text-v-muted">Loading diff…</div>
                  <div v-else-if="diffError" class="text-xs text-v-muted">Could not load diff: {{ diffError }}</div>
                  <template v-else>
                    <div v-if="behindTotal > 0" class="diff-behind-warning">
                      ⚠ {{ behindTotal }} commit{{ behindTotal === 1 ? '' : 's' }} behind the default branch — sync to rebase.
                    </div>
                    <div v-if="diffFiles.length === 0" class="text-xs text-v-muted">No changes</div>
                    <template v-for="(f, fi) in diffFiles" :key="fi">
                      <div v-if="showWorkspaceHeader(fi)" class="diff-workspace-header">{{ f.workspace }}</div>
                      <details class="diff-file" open>
                        <summary class="diff-file-summary">
                          <span class="diff-filename">{{ f.filename }}</span>
                          <span class="diff-stats">
                            <span v-if="f.adds" class="diff-add">+{{ f.adds }}</span>
                            <span v-if="f.dels" class="diff-del">&minus;{{ f.dels }}</span>
                          </span>
                        </summary>
                        <pre class="diff-block diff-block-modal"><template v-for="(ln, li) in f.lines" :key="li"><span class="diff-line" :class="lineClass(ln.kind)">{{ ln.text }}</span>
</template></pre>
                      </details>
                    </template>
                  </template>
                </div>

                <!-- EVENTS tab -->
                <div data-main-tab-section="events">
                  <h3 class="section-title">Events</h3>
                  <div v-if="eventsLoading" class="text-xs text-v-muted mb-4">Loading events…</div>
                  <div v-else-if="!visibleEvents.length" class="text-xs text-v-muted mb-4">No events recorded.</div>
                  <ul v-else class="event-list mb-4">
                    <li
                      v-for="e in visibleEvents"
                      :key="e.id"
                      class="event-row"
                      :data-event-type="e.event_type"
                    >
                      <span class="event-row__type">{{ e.event_type }}</span>
                      <span class="event-row__summary">{{ eventSummary(e) }}</span>
                      <span class="event-row__time">{{ timeStr(e.created_at) }}</span>
                    </li>
                  </ul>

                  <h3 class="section-title">Usage</h3>
                  <div class="usage-grid mb-4">
                    <div class="flex justify-between">
                      <span class="usage-label">Input tokens</span>
                      <span class="usage-value">{{ tokenCount(task.usage?.input_tokens) }}</span>
                    </div>
                    <div class="flex justify-between">
                      <span class="usage-label">Output tokens</span>
                      <span class="usage-value">{{ tokenCount(task.usage?.output_tokens) }}</span>
                    </div>
                    <div class="flex justify-between">
                      <span class="usage-label">Cache read</span>
                      <span class="usage-value">{{ tokenCount(task.usage?.cache_read_input_tokens) }}</span>
                    </div>
                    <div class="flex justify-between">
                      <span class="usage-label">Cache creation</span>
                      <span class="usage-value">{{ tokenCount(task.usage?.cache_creation_input_tokens) }}</span>
                    </div>
                    <div class="flex justify-between" style="grid-column: span 2; padding-top: 4px; border-top: 1px solid var(--border); margin-top: 4px;">
                      <span class="usage-label">Total cost</span>
                      <span class="usage-value">{{ costDisplay }}</span>
                    </div>
                  </div>

                  <h3 class="section-title">Timeline</h3>
                  <div class="usage-grid">
                    <div class="flex justify-between">
                      <span class="usage-label">Created</span>
                      <span class="usage-value">{{ timeStr(task.created_at) }}</span>
                    </div>
                    <div class="flex justify-between">
                      <span class="usage-label">Updated</span>
                      <span class="usage-value">{{ timeStr(task.updated_at) }}</span>
                    </div>
                    <div class="flex justify-between">
                      <span class="usage-label">Turns</span>
                      <span class="usage-value">{{ task.turns ?? 0 }}</span>
                    </div>
                  </div>

                  <!-- Per-sub-agent usage breakdown -->
                  <template v-if="usageBreakdown.length">
                    <h3 class="section-title" style="margin-top: 16px;">Usage by agent</h3>
                    <table class="usage-breakdown">
                      <thead>
                        <tr><th>Agent</th><th>In</th><th>Out</th><th>Cost</th></tr>
                      </thead>
                      <tbody>
                        <tr v-for="row in usageBreakdown" :key="row.label">
                          <td>{{ row.label }}</td>
                          <td>{{ tokenCount(row.input) }}</td>
                          <td>{{ tokenCount(row.output) }}</td>
                          <td>{{ row.cost > 0 ? '$' + row.cost.toFixed(4) : '—' }}</td>
                        </tr>
                      </tbody>
                    </table>
                  </template>

                  <!-- Retry history (past retired attempts) -->
                  <template v-if="retryHistory.length">
                    <h3 class="section-title" style="margin-top: 16px;">Retry history</h3>
                    <div v-for="(r, i) in retryHistory" :key="i" class="retry-record">
                      <div class="retry-record__head">
                        <span class="badge" :class="badgeClassFor(r.status)">{{ r.status }}</span>
                        <span v-if="r.failure_category" class="retry-record__cat">{{ r.failure_category }}</span>
                        <span class="retry-record__meta">{{ r.turns }} turn{{ r.turns === 1 ? '' : 's' }} · ${{ r.cost_usd.toFixed(4) }} · {{ timeStr(r.retired_at) }}</span>
                      </div>
                      <details v-if="r.result" class="retry-record__detail">
                        <summary>result</summary>
                        <pre>{{ r.result }}</pre>
                      </details>
                    </div>
                  </template>

                  <!-- Prompt history (prior iterations) -->
                  <template v-if="promptHistory.length">
                    <h3 class="section-title" style="margin-top: 16px;">Prompt history</h3>
                    <details v-for="(p, i) in promptHistory" :key="i" class="prompt-record">
                      <summary>#{{ i + 1 }}</summary>
                      <pre>{{ p }}</pre>
                    </details>
                  </template>
                </div>

                <!-- RESULTS tab (multi-turn) -->
                <div data-main-tab-section="results">
                  <div v-if="resultsLoading" class="text-xs text-v-secondary">Loading results…</div>
                  <div v-else-if="resultsError" class="text-xs" style="color: var(--err, #c0392b);">{{ resultsError }}</div>
                  <div v-else-if="!implResults.length && !testResults.length" class="text-xs text-v-muted">No turn results yet.</div>
                  <template v-else>
                    <template v-if="implResults.length">
                      <h3 class="section-title">Implementation</h3>
                      <!-- Newest turn expanded; older turns collapse into
                           <details> (mirrors ui/js/modal-results.js). -->
                      <details
                        v-for="(entry, idx) in implResults"
                        :key="`impl-${entry.turn}`"
                        class="result-entry"
                        :open="idx === 0"
                      >
                        <summary class="result-entry-summary">
                          <div class="result-entry-labels">
                            <span v-if="entry.type === 'plan'" class="result-type-badge result-type-plan">Plan</span>
                            <span v-if="implResults.length > 1" class="result-turn-label">Turn {{ entry.turn }}</span>
                          </div>
                        </summary>
                        <div class="result-entry-actions flex items-center gap-1.5">
                          <button type="button" class="btn-icon" @click="copyResult(entry)">Copy</button>
                          <button type="button" class="btn-icon" @click="entry.showRaw = !entry.showRaw">{{ entry.showRaw ? 'Rendered' : 'Raw' }}</button>
                        </div>
                        <pre v-if="entry.showRaw" class="result-entry-body">{{ entry.text }}</pre>
                        <!-- eslint-disable-next-line vue/no-v-html — renderMarkdown sanitises -->
                        <div v-else class="result-entry-body prose-content" v-html="renderResultMarkdown(entry.text)" />
                      </details>
                    </template>
                    <template v-if="testResults.length">
                      <h3 class="section-title" style="margin-top: 16px;">Testing</h3>
                      <details
                        v-for="(entry, idx) in testResults"
                        :key="`test-${entry.turn}`"
                        class="result-entry"
                        :open="idx === 0"
                      >
                        <summary class="result-entry-summary">
                          <div class="result-entry-labels">
                            <span v-if="entry.type === 'plan'" class="result-type-badge result-type-plan">Plan</span>
                            <span class="result-turn-label">Turn {{ entry.turn }}</span>
                          </div>
                        </summary>
                        <div class="result-entry-actions flex items-center gap-1.5">
                          <button type="button" class="btn-icon" @click="copyResult(entry)">Copy</button>
                          <button type="button" class="btn-icon" @click="entry.showRaw = !entry.showRaw">{{ entry.showRaw ? 'Rendered' : 'Raw' }}</button>
                        </div>
                        <pre v-if="entry.showRaw" class="result-entry-body">{{ entry.text }}</pre>
                        <!-- eslint-disable-next-line vue/no-v-html — renderMarkdown sanitises -->
                        <div v-else class="result-entry-body prose-content" v-html="renderResultMarkdown(entry.text)" />
                      </details>
                    </template>
                  </template>
                </div>

                <!-- TIMELINE tab (span flamegraph) -->
                <div data-main-tab-section="timeline">
                  <h3 class="section-title">Timeline</h3>
                  <div v-if="spansLoading" class="text-xs text-v-secondary">Loading spans…</div>
                  <div v-else-if="spansError" class="text-xs" style="color: var(--err, #c0392b);">{{ spansError }}</div>
                  <SpanFlamegraph v-else :spans="spans" />
                </div>
              </div>
            </div>

            <!-- Right aside -->
            <aside class="modal-aside">
              <div v-if="blockedBy.length" class="mdl-section modal-aside__deps">
                <div class="mdl-h">
                  Blocked by
                  <span v-if="blockedByUnmet > 0" class="deps-summary">waiting on {{ blockedByUnmet }} of {{ blockedBy.length }}</span>
                  <span v-else class="deps-summary deps-summary--ready">all satisfied</span>
                </div>
                <button
                  v-for="d in blockedBy"
                  :key="d.id"
                  type="button"
                  class="dep-row"
                  :title="`Open ${d.label}`"
                  @click="openDep(d.id)"
                >
                  <span class="badge" :class="badgeClassFor(d.status)">{{ d.status === 'in_progress' ? 'in progress' : d.status }}</span>
                  <span class="dep-row__label">{{ d.label }}</span>
                </button>
              </div>

              <div class="mdl-section modal-aside__actions">
                <div class="mdl-h">Actions</div>

                <div v-if="isBacklog">
                  <button type="button" class="aside-action aside-action--primary" @click="startTask">
                    <span class="aside-action__icon" aria-hidden="true">&#9654;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Start task</span>
                      <span class="aside-action__hint">move to In Progress</span>
                    </span>
                  </button>
                  <button v-if="!editingBacklog" type="button" class="aside-action" @click="openBacklogEdit">
                    <span class="aside-action__icon" aria-hidden="true">&#9998;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Edit settings</span>
                      <span class="aside-action__hint">timeout, tags, model, budget</span>
                    </span>
                  </button>
                  <div v-if="editingBacklog" class="backlog-edit">
                    <label class="backlog-edit__field">
                      <span>Timeout (min)</span>
                      <input v-model.number="editTimeout" type="number" min="1" placeholder="15" />
                    </label>
                    <label class="backlog-edit__field">
                      <span>Model</span>
                      <input v-model="editModel" type="text" placeholder="override model" />
                    </label>
                    <label class="backlog-edit__field">
                      <span>Tags</span>
                      <input v-model="editTags" type="text" placeholder="comma,separated" />
                    </label>
                    <label class="backlog-edit__field">
                      <span>Max $</span>
                      <input v-model.number="editMaxCost" type="number" min="0" step="0.5" placeholder="USD (0 = unlimited)" />
                    </label>
                    <label class="backlog-edit__field">
                      <span>Max tokens</span>
                      <input v-model.number="editMaxTokens" type="number" min="0" step="1000" placeholder="0 = unlimited" />
                    </label>
                    <div class="backlog-edit__actions">
                      <button type="button" class="composer__btn composer__btn--ghost" :disabled="editSaving" @click="editingBacklog = false">Cancel</button>
                      <button type="button" class="composer__btn composer__btn--primary" :disabled="editSaving" @click="saveBacklogEdit">{{ editSaving ? 'Saving…' : 'Save' }}</button>
                    </div>
                  </div>
                </div>

                <div v-if="isWaiting">
                  <button type="button" class="aside-action aside-action--success" @click="completeTask">
                    <span class="aside-action__icon" aria-hidden="true">&#10003;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Mark as Done</span>
                      <span class="aside-action__hint">commit and close</span>
                    </span>
                  </button>
                </div>

                <div v-if="isFailed && task.session_id">
                  <button type="button" class="aside-action" @click="resumeTask">
                    <span class="aside-action__icon" aria-hidden="true">&#8635;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Resume</span>
                      <span class="aside-action__hint">continue existing session</span>
                    </span>
                  </button>
                </div>

                <div v-if="isWaiting || isDone || isFailed">
                  <button type="button" class="aside-action" @click="testTask">
                    <span class="aside-action__icon" aria-hidden="true">&#9654;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Test</span>
                      <span class="aside-action__hint">run test verification</span>
                    </span>
                  </button>
                </div>

                <div v-if="budgetExceeded && (isWaiting || isFailed)">
                  <button type="button" class="aside-action" @click="raiseBudget">
                    <span class="aside-action__icon" aria-hidden="true">&#36;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Raise budget</span>
                      <span class="aside-action__hint">unblock and resume</span>
                    </span>
                  </button>
                </div>

                <div v-if="isWaiting || isFailed">
                  <button type="button" class="aside-action" @click="syncTask">
                    <span class="aside-action__icon" aria-hidden="true">&#8645;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Sync</span>
                      <span class="aside-action__hint">rebase onto default branch</span>
                    </span>
                  </button>
                </div>

                <div v-if="isFailed || isCancelled">
                  <button type="button" class="aside-action" @click="retryTask">
                    <span class="aside-action__icon" aria-hidden="true">&#8634;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Retry</span>
                      <span class="aside-action__hint">move back to Backlog</span>
                    </span>
                  </button>
                </div>

                <div v-if="(isDone || isCancelled) && !isArchived">
                  <button type="button" class="aside-action" @click="archiveTask">
                    <span class="aside-action__icon" aria-hidden="true">&#128229;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Archive</span>
                      <span class="aside-action__hint">hide from board</span>
                    </span>
                  </button>
                </div>

                <div v-if="isArchived">
                  <button type="button" class="aside-action" @click="unarchiveTask">
                    <span class="aside-action__icon" aria-hidden="true">&#128228;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Unarchive</span>
                      <span class="aside-action__hint">restore to board</span>
                    </span>
                  </button>
                </div>

                <div v-if="isInProgress || isWaiting">
                  <button
                    type="button"
                    class="aside-action aside-action--warn"
                    :disabled="cancelling"
                    @click="cancelTask"
                  >
                    <span class="aside-action__icon" aria-hidden="true">{{ cancelling ? '…' : '⏹' }}</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">{{ cancelling ? 'Shutting down…' : 'Cancel' }}</span>
                      <span class="aside-action__hint">{{ cancelling ? 'stopping container' : 'discard changes' }}</span>
                    </span>
                  </button>
                </div>

                <div>
                  <button type="button" class="aside-action aside-action--danger" @click="deleteTask">
                    <span class="aside-action__icon" aria-hidden="true">&#128465;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Delete</span>
                      <span class="aside-action__hint">remove permanently</span>
                    </span>
                  </button>
                </div>
              </div>

              <div class="mdl-section">
                <div class="mdl-h">Agent</div>
                <div class="row">
                  <span class="k">sandbox</span>
                  <span class="v">{{ task.sandbox || '—' }}</span>
                </div>
                <div class="row">
                  <span class="k">model</span>
                  <span class="v">{{ task.model || '—' }}</span>
                </div>
                <div class="row">
                  <span class="k">status</span>
                  <span class="v">{{ status }}</span>
                </div>
                <div class="row">
                  <span class="k">elapsed</span>
                  <span class="v">{{ elapsedDisplay }}</span>
                </div>
              </div>

              <div class="mdl-section">
                <div class="mdl-h">Budget</div>
                <div class="row">
                  <span class="k">tokens</span>
                  <span class="v mono">{{ tokenCount((task.usage?.input_tokens || 0) + (task.usage?.output_tokens || 0)) }}</span>
                </div>
                <div class="row">
                  <span class="k">cost</span>
                  <span class="v mono">{{ costDisplay }}</span>
                </div>
                <div class="row">
                  <span class="k">turns</span>
                  <span class="v mono">{{ task.turns ?? 0 }}</span>
                </div>
              </div>
            </aside>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.modal-overlay {
  position: fixed;
  inset: 0;
  z-index: 50;
}
.modal-close-btn {
  background: none;
  border: none;
  cursor: pointer;
  font-size: 22px;
  line-height: 1;
  color: var(--text-muted);
  padding: 0 4px;
}
.modal-close-btn:hover { color: var(--text); }

.modal-title {
  font-size: 15px;
  font-weight: 600;
  color: var(--text);
  margin: 0 0 12px 0;
  line-height: 1.4;
}

.text-v-muted { color: var(--text-muted); }
.text-xs { font-size: 11px; }
.font-mono { font-family: var(--font-mono); }

.flex { display: flex; }
.items-center { align-items: center; }
.items-start { align-items: flex-start; }
.justify-between { justify-content: space-between; }
.gap-2 { gap: 8px; }
.gap-3 { gap: 12px; }
.mb-4 { margin-bottom: 16px; }
.mt-2 { margin-top: 8px; }
.fixed { position: fixed; }
.inset-0 { inset: 0; }
.z-50 { z-index: 50; }
.p-4 { padding: 16px; }
.p-6 { padding: 24px; }

.field {
  width: 100%;
  background: var(--bg-input);
  border: 1px solid var(--border);
  color: var(--text);
  border-radius: 6px;
  padding: 8px 10px;
  font-family: var(--font-sans);
  font-size: 13px;
  outline: none;
  resize: vertical;
  box-sizing: border-box;
}
.field:focus { border-color: var(--accent); }

.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 14px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text);
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}
.btn:hover { background: var(--bg-hover); }
.btn:disabled { opacity: 0.4; cursor: not-allowed; }
.btn-yellow {
  background: var(--warn);
  border-color: var(--warn);
  color: #fff;
}
.btn-yellow:hover { opacity: 0.9; background: var(--warn); }

.pulse-dot {
  display: inline-block;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--ok);
  margin-left: 6px;
  animation: pulse-anim 1.5s infinite;
}
@keyframes pulse-anim {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.3; }
}

/* Feedback @-mention dropdown. */
.fb-wrap { position: relative; }
.fb-mentions {
  position: absolute;
  left: 0; right: 0; top: 100%;
  z-index: 30;
  margin: 2px 0 0;
  padding: 4px;
  list-style: none;
  max-height: 200px;
  overflow-y: auto;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 6px;
  box-shadow: 0 8px 24px rgba(0,0,0,0.18);
}
.fb-mention {
  padding: 4px 8px;
  font-size: 12px;
  font-family: var(--font-mono);
  border-radius: 4px;
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.fb-mention.active, .fb-mention:hover { background: var(--bg-hover); }

/* Oversight summary phases. */
.ta-oversight {
  margin-bottom: 14px;
  padding: 10px 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-input);
}
.ta-oversight__label {
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-muted);
  margin-bottom: 8px;
}
.ta-oversight__phase { padding: 6px 0; border-top: 1px solid var(--border); }
.ta-oversight__phase:first-of-type { border-top: none; }
.ta-oversight__title { font-size: 13px; font-weight: 600; color: var(--text); }
.ta-oversight__summary { font-size: 12px; color: var(--text); margin-top: 2px; line-height: 1.5; }
.ta-oversight__tools { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 4px; }
.ta-oversight__tool {
  font-size: 10px;
  font-family: var(--font-mono);
  padding: 1px 6px;
  border-radius: 4px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  color: var(--text-muted);
}

/* Activity tab search bar + truncation notice. */
.ta-activity-search {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 4px 0 6px;
}
.ta-activity-search__input {
  flex: 1;
  background: var(--bg-input);
  border: 1px solid var(--border);
  color: var(--text);
  border-radius: 6px;
  padding: 4px 8px;
  font-size: 12px;
}
.ta-activity-search__count {
  font-size: 11px;
  color: var(--text-muted);
  font-family: var(--font-mono);
}
.ta-activity-truncated {
  font-size: 11px;
  color: var(--text-muted);
  font-style: italic;
  margin-bottom: 4px;
}
.ta-activity-truncated a {
  margin-left: 6px;
  color: var(--accent);
  text-decoration: underline;
  font-style: normal;
}
.ta-activity-truncated--warn {
  font-style: normal;
  color: var(--warn, #b8860b);
  background: rgba(217, 119, 87, 0.08);
  border: 1px solid rgba(217, 119, 87, 0.25);
  border-radius: 6px;
  padding: 6px 8px;
}

/* Backlog edit form inside the right aside. */
.backlog-edit {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px 6px;
  border-top: 1px solid var(--border);
  margin-top: 6px;
}
.backlog-edit__field {
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-size: 11px;
  color: var(--text-muted);
}
.backlog-edit__field input {
  background: var(--bg-input);
  border: 1px solid var(--border);
  color: var(--text);
  border-radius: 6px;
  padding: 4px 8px;
  font-size: 12px;
  font-family: var(--font-sans);
}
.backlog-edit__actions {
  display: flex;
  justify-content: flex-end;
  gap: 6px;
  margin-top: 4px;
}

/* "Blocked by" dependency list in the aside. */
.modal-aside__deps .deps-summary {
  font-size: 10px;
  font-weight: 500;
  color: var(--text-muted);
  margin-left: 6px;
  text-transform: none;
  letter-spacing: 0;
}
.modal-aside__deps .deps-summary--ready { color: var(--green, #22c55e); }
.dep-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  background: transparent;
  border: none;
  padding: 4px 4px;
  border-radius: 6px;
  cursor: pointer;
  text-align: left;
  color: var(--text);
}
.dep-row:hover { background: var(--bg-hover); }
.dep-row__label { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; }

/* Usage-by-agent breakdown table. */
.usage-breakdown { width: 100%; border-collapse: collapse; font-size: 11px; }
.usage-breakdown th {
  text-align: right; padding: 3px 8px; color: var(--text-muted);
  font-weight: 600; text-transform: uppercase; letter-spacing: 0.03em; font-size: 10px;
  border-bottom: 1px solid var(--border);
}
.usage-breakdown th:first-child, .usage-breakdown td:first-child { text-align: left; }
.usage-breakdown td { padding: 3px 8px; text-align: right; font-variant-numeric: tabular-nums; }

/* Retry + prompt history records in the Events tab. */
.retry-record { border-top: 1px solid var(--border); padding: 6px 0; }
.retry-record__head { display: flex; align-items: center; gap: 8px; font-size: 11px; }
.retry-record__cat { color: var(--err, #c0392b); font-family: var(--font-mono); font-size: 10px; }
.retry-record__meta { margin-left: auto; color: var(--text-muted); font-variant-numeric: tabular-nums; }
.retry-record__detail pre, .prompt-record pre {
  white-space: pre-wrap; font-size: 11px; margin: 4px 0 0; color: var(--text-secondary);
  max-height: 220px; overflow: auto;
}
.prompt-record { border-top: 1px solid var(--border); padding: 6px 0; font-size: 11px; }
.prompt-record summary, .retry-record__detail summary { cursor: pointer; color: var(--text-muted); }

/* Event timeline rows in the Events tab. */
.event-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; }
.event-row {
  display: grid;
  grid-template-columns: 92px 1fr auto;
  align-items: baseline;
  gap: 8px;
  padding: 3px 6px;
  font-size: 11px;
  border-radius: 4px;
}
.event-row:hover { background: var(--bg-hover); }
.event-row__type {
  font-family: var(--font-mono);
  font-size: 10px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.event-row[data-event-type="error"] .event-row__type { color: var(--err, #c0392b); }
.event-row[data-event-type="state_change"] .event-row__type { color: var(--accent); }
.event-row__summary {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text);
}
.event-row__time { color: var(--text-muted); font-variant-numeric: tabular-nums; white-space: nowrap; }

/* Pretty agent-activity rows (mirrors the planning chat's pcp-activity). */
.ta-activity-log {
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-family: var(--font-mono);
  font-size: 12px;
  max-height: 60vh;
  overflow-y: auto;
}
.ta-activity-row {
  display: grid;
  grid-template-columns: 16px auto 1fr;
  align-items: baseline;
  gap: 6px;
  padding: 2px 4px;
  border-radius: 4px;
}
.ta-activity-row:hover { background: var(--bg-hover); }
.ta-activity-icon { text-align: center; opacity: 0.8; }
.ta-activity-label { font-weight: 600; color: var(--text); }
.ta-activity-row--tool .ta-activity-label { color: var(--accent); }
.ta-activity-row--tool_result .ta-activity-label { color: var(--ok); }
.ta-activity-row--thinking .ta-activity-label { color: var(--warn); font-style: italic; }
.ta-activity-summary {
  color: var(--text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ta-activity-detail { grid-column: 2 / -1; }
.ta-activity-detail summary { cursor: pointer; color: var(--text-muted); font-size: 11px; }
.ta-activity-detail pre {
  margin: 4px 0 0;
  padding: 6px 8px;
  background: var(--bg-input);
  border-radius: 4px;
  white-space: pre-wrap;
  word-break: break-word;
  max-height: 240px;
  overflow: auto;
}
</style>
