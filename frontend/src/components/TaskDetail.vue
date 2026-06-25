<script setup lang="ts">
import { ref, computed, nextTick, watch, onMounted, onUnmounted } from 'vue';
import { api } from '../api/client';
import { useTaskActivity } from '../composables/useTaskActivity';
import { parseDiffFiles, type DiffFile } from '../lib/diff';
import { highlightDiffFile, type HighlightedDiffLine } from '../lib/diffHighlight';
import type { ActivityRow } from '../lib/prettyNdjson';
import type { Task } from '../api/types';
import { useMentions } from '../composables/useMentions';
import { useDialogStore } from '../stores/dialog';
import { useToastStore } from '../stores/toast';
import { useTaskStore } from '../stores/tasks';
import { useRouter } from 'vue-router';
import SpanFlamegraph from './SpanFlamegraph.vue';
import DependencyPicker from './DependencyPicker.vue';
import AppSelect from './AppSelect.vue';
import type { SpanResult, TurnUsageRecord } from '../lib/flamegraph';
import { detectResultType } from '../lib/resultType';
// Re-imported as a local binding so the template can call renderMarkdown()
// directly inside the Results tab.
import { renderMarkdown as renderResultMarkdown } from '../lib/markdown';
import { ansiToHtml } from '../lib/ansi';
import { useFocusTrap } from '../composables/useFocusTrap';

const props = defineProps<{ task: Task; initialTab?: string }>();
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

// Execution-environment provenance rows (harness, model, API endpoint,
// recorded time).
const envRows = computed<{ label: string; value: string; mono?: boolean }[]>(() => {
  const e = props.task.environment;
  if (!e) return [];
  const rows: { label: string; value: string; mono?: boolean }[] = [];
  rows.push({ label: 'Harness', value: e.sandbox || '(default)' });
  rows.push({ label: 'Model', value: e.model_name || '(unknown)' });
  rows.push({ label: 'API endpoint', value: e.api_base_url || '(default)' });
  if (e.recorded_at) rows.push({ label: 'Recorded', value: relativeTime(e.recorded_at) });
  return rows;
});
function openDep(id: string) {
  detailRouter.push({ path: '/', query: { task: id } });
}

// Source spec link (Links → spec). The basename is shown as the label; the
// click deep-links to /plan focusing the spec (PlanPage honours ?spec=<path>).
const specSourcePath = computed(() => props.task.spec_source_path ?? '');
const specSourceLabel = computed(() => {
  const p = specSourcePath.value;
  if (!p) return '';
  const parts = p.split('/');
  return parts[parts.length - 1] || p;
});
function openSpec() {
  if (!specSourcePath.value) return;
  detailRouter.push({ path: '/plan', query: { spec: specSourcePath.value } });
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

type MainTab = 'spec' | 'activity' | 'changes' | 'verification' | 'events' | 'timeline';
const MAIN_TABS: readonly MainTab[] = ['spec', 'activity', 'changes', 'verification', 'events', 'timeline'];
// Honour an initial tab (command-palette tab-switch jumps / deep links).
const mainTab = ref<MainTab>(
  MAIN_TABS.includes(props.initialTab as MainTab) ? (props.initialTab as MainTab) : 'spec',
);

// --- Changes (diff) tab ---
const diffFiles = ref<DiffFile[]>([]);
// Per-file highlight.js token HTML (null when the language is unknown — the
// template then falls back to plain text). Indexed parallel to diffFiles.
const diffHighlights = computed<(HighlightedDiffLine[] | null)[]>(
  () => diffFiles.value.map((f) => highlightDiffFile(f)),
);
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
    // The Verification tab shows only the test/verify-phase transcript: turns at
    // or after task.test_run_start_turn (1-based) belong to the test agent.
    // Earlier (implementation) turns live in Activity and Spec, so they're not
    // repeated here.
    const startTurn = props.task.test_run_start_turn ?? 0;
    const splitIdx = startTurn > 0 ? Math.min(startTurn - 1, outputs.length) : outputs.length;
    const tests = outputs.slice(splitIdx);
    testResults.value = tests.map((text, i) => ({
      turn: splitIdx + i + 1,
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

// Spec/Result markdown helper actions (toggle rendered/raw + copy), mirroring
// ui/js/markdown.js toggleModalSection / copyModalText.
const specShowRaw = ref(false);
const resultShowRaw = ref(false);
function copyText(text: string) {
  void navigator.clipboard.writeText(text);
  toast.push('Copied to clipboard', { kind: 'success', timeout: 2000 });
}
const specPromptHtml = computed(() => renderResultMarkdown(props.task.prompt || ''));
const specResultHtml = computed(() => renderResultMarkdown(props.task.result || ''));

// --- Timeline (span flamegraph) tab ---

const spans = ref<SpanResult[]>([]);
const turnUsages = ref<TurnUsageRecord[]>([]);
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
    // Turn-usage powers the cumulative cost chart overlay (best-effort).
    try {
      const usage = await api<TurnUsageRecord[]>('GET', `/api/tasks/${props.task.id}/turn-usage`);
      turnUsages.value = Array.isArray(usage) ? usage : [];
    } catch { turnUsages.value = []; }
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

// Collapse consecutive events with the same type + summary into one row with a
// count, so a burst of identical "system" events reads as "system ×7" instead
// of a wall of repeated rows.
interface GroupedEvent {
  ref: TaskEvent;
  summary: string;
  count: number;
}
const groupedEvents = computed<GroupedEvent[]>(() => {
  const out: GroupedEvent[] = [];
  for (const e of visibleEvents.value) {
    const summary = eventSummary(e);
    const last = out[out.length - 1];
    if (last && last.ref.event_type === e.event_type && last.summary === summary) {
      last.count++;
      last.ref = e; // keep the most recent timestamp for the group
    } else {
      out.push({ ref: e, summary, count: 1 });
    }
  }
  return out;
});

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

function fetchForTab(t: MainTab) {
  if (t === 'changes' && !diffFetched.value) fetchDiff();
  if (t === 'activity') fetchOversight();
  if (t === 'verification' && !resultsFetched.value) fetchResults();
  if (t === 'timeline' && !spansFetched.value) fetchSpans();
  if (t === 'events' && !eventsFetched.value) fetchEvents();
}
watch(mainTab, fetchForTab);
// When opened directly on a data tab (command-palette jump / deep link), the
// change watcher above never fires, so kick off that tab's fetch on mount.
onMounted(() => { if (mainTab.value !== 'spec') fetchForTab(mainTab.value); });

// Refetch per-tab data when navigating to a different task while a data
// tab stays selected (e.g. via deep-link or sidebar nav).
watch(
  () => props.task?.id,
  () => {
    spansFetched.value = false; spans.value = []; turnUsages.value = [];
    resultsFetched.value = false; testResults.value = [];
    eventsFetched.value = false; events.value = [];
    diffFetched.value = false; diffFiles.value = []; behindCounts.value = {};
    if (mainTab.value === 'timeline') fetchSpans();
    if (mainTab.value === 'verification') fetchResults();
    if (mainTab.value === 'events') fetchEvents();
    if (mainTab.value === 'changes') fetchDiff();
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
// Test-phase oversight (the test agent's run), shown as a parallel "Testing"
// section. Sourced from /oversight?phase=test.
const testOversightPhases = ref<OversightPhase[]>([]);
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
    testOversightPhases.value = [];
  }
  // Test-phase oversight (best-effort; only present once a test agent has run).
  try {
    const t = await api<{ phases?: OversightPhase[] }>('GET', `/api/tasks/${props.task.id}/oversight?phase=test`);
    testOversightPhases.value = t?.phases ?? [];
  } catch { testOversightPhases.value = []; }
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

// activity is reassigned wholesale per chunk (activity.value = [...]), never
// mutated in place, so a shallow watch already fires on every update. A deep
// watch over up to ACTIVITY_MAX_ROWS objects would only add traversal cost.
watch(activity, async () => {
  await nextTick();
  if (logContainer.value) {
    logContainer.value.scrollTop = logContainer.value.scrollHeight;
  }
});

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

const gitBranches = computed(() => {
  const repos = Object.keys(props.task.worktree_paths || {});
  return repos.length ? repos.join(', ') : '—';
});

const gitWorktrees = computed(() => {
  const n = Object.keys(props.task.worktree_paths || {}).length;
  return n ? `${n} ${n === 1 ? 'repo' : 'repos'}` : 'none';
});

const dependsOnDisplay = computed(() => {
  const n = (props.task.depends_on || []).length;
  return n ? `${n} ${n === 1 ? 'task' : 'tasks'}` : 'none';
});

// Budget usage as a percentage of the configured cost/token cap (0 when no cap).
const budgetPct = computed(() => {
  const u = props.task.usage;
  if ((props.task.max_cost_usd ?? 0) > 0 && u?.cost_usd) {
    return Math.min(1, u.cost_usd / props.task.max_cost_usd!) * 100;
  }
  if ((props.task.max_input_tokens ?? 0) > 0 && u?.input_tokens) {
    return Math.min(1, u.input_tokens / props.task.max_input_tokens!) * 100;
  }
  return 0;
});

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
    await api('PATCH', `/api/tasks/${props.task.id}`, { status: 'cancelled' });
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
const editPrompt = ref('');
const editPromptPreview = ref(false);
const editTimeout = ref<number | null>(null);
const editModel = ref('');
const editSandbox = ref('');
const EDIT_SANDBOX_OPTIONS = [
  { value: '', label: 'Default (agent)' },
  { value: 'claude', label: 'Claude' },
  { value: 'codex', label: 'Codex' },
];
const editTags = ref('');
const editDeps = ref<string[]>([]);
const editScheduledAt = ref('');
const editMaxCost = ref<number | null>(null);
const editMaxTokens = ref<number | null>(null);
const editSaving = ref(false);

const editPromptHtml = computed(() => renderResultMarkdown(editPrompt.value || ''));

// Convert an ISO timestamp to the value a <input type="datetime-local"> expects
// (local YYYY-MM-DDTHH:MM), mirroring modal-core.js.
function toDatetimeLocal(iso: string | null | undefined): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '';
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function openBacklogEdit() {
  const t = props.task;
  editPrompt.value = t.prompt ?? '';
  editPromptPreview.value = false;
  editTimeout.value = t.timeout > 0 ? Math.round(t.timeout / 60) : null;
  editModel.value = t.model ?? '';
  editSandbox.value = t.sandbox ?? '';
  editTags.value = (t.tags ?? []).join(', ');
  editDeps.value = [...(t.depends_on ?? [])];
  editScheduledAt.value = toDatetimeLocal(t.scheduled_at);
  editMaxCost.value = t.max_cost_usd && t.max_cost_usd > 0 ? t.max_cost_usd : null;
  editMaxTokens.value = t.max_input_tokens && t.max_input_tokens > 0 ? t.max_input_tokens : null;
  editingBacklog.value = true;
}

async function saveBacklogEdit() {
  editSaving.value = true;
  try {
    const t = props.task;
    const patch: Record<string, unknown> = {};
    if (editPrompt.value.trim() !== (t.prompt ?? '').trim()) patch.prompt = editPrompt.value.trim();
    if (editTimeout.value !== null && editTimeout.value > 0) patch.timeout = editTimeout.value * 60;
    if (editModel.value.trim() !== (t.model ?? '').trim()) patch.model = editModel.value.trim();
    if (editSandbox.value !== (t.sandbox ?? '')) patch.sandbox = editSandbox.value;
    const parsedTags = editTags.value.split(',').map((x) => x.trim()).filter(Boolean);
    if (JSON.stringify(parsedTags) !== JSON.stringify(t.tags ?? [])) patch.tags = parsedTags;
    if (JSON.stringify(editDeps.value) !== JSON.stringify(t.depends_on ?? [])) patch.depends_on = [...editDeps.value];
    const nextScheduled = editScheduledAt.value ? new Date(editScheduledAt.value).toISOString() : null;
    if ((nextScheduled ?? '') !== (t.scheduled_at ?? '')) patch.scheduled_at = nextScheduled;
    if ((editMaxCost.value ?? 0) !== (t.max_cost_usd ?? 0)) patch.max_cost_usd = editMaxCost.value ?? 0;
    if ((editMaxTokens.value ?? 0) !== (t.max_input_tokens ?? 0)) patch.max_input_tokens = editMaxTokens.value ?? 0;
    if (Object.keys(patch).length === 0) { editingBacklog.value = false; return; }
    await api('PATCH', `/api/tasks/${t.id}`, patch);
    toast.push('Task updated', { kind: 'success' });
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
  await api('PATCH', `/api/tasks/${id}`, { archived: true });
  toast.pushWithAction('Task archived', 'Undo', () => {
    api('PATCH', `/api/tasks/${id}`, { archived: false }).catch((e) => console.error('unarchive:', e));
  }, { kind: 'success' });
}
async function unarchiveTask() {
  await api('PATCH', `/api/tasks/${props.task.id}`, { archived: false });
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
              :class="{ active: mainTab === 'verification' }"
              role="tab"
              :aria-selected="mainTab === 'verification'"
              aria-controls="modal-row"
              @click="mainTab = 'verification'"
            >Verification</button>
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
                  <div class="md-section-head">
                    <h3 class="section-title">Spec</h3>
                    <span class="md-section-actions">
                      <button type="button" class="btn-icon" @click="copyText(task.prompt)">Copy</button>
                      <button type="button" class="btn-icon" @click="specShowRaw = !specShowRaw">{{ specShowRaw ? 'Rendered' : 'Raw' }}</button>
                    </span>
                  </div>
                  <pre v-if="specShowRaw" class="code-block mb-4">{{ task.prompt }}</pre>
                  <!-- eslint-disable-next-line vue/no-v-html — renderMarkdown sanitises -->
                  <div v-else class="prose-content mb-4" v-html="specPromptHtml"></div>

                  <template v-if="task.result">
                    <div class="md-section-head">
                      <h3 class="section-title">Result</h3>
                      <span class="md-section-actions">
                        <button type="button" class="btn-icon" @click="copyText(task.result || '')">Copy</button>
                        <button type="button" class="btn-icon" @click="resultShowRaw = !resultShowRaw">{{ resultShowRaw ? 'Rendered' : 'Raw' }}</button>
                      </span>
                    </div>
                    <pre v-if="resultShowRaw" class="code-block mb-4">{{ task.result }}</pre>
                    <!-- eslint-disable-next-line vue/no-v-html — renderMarkdown sanitises -->
                    <div v-else class="prose-content mb-4" v-html="specResultHtml"></div>
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
                      <div v-if="testOversightPhases.length" class="ta-oversight" style="margin-top: 10px;">
                        <div class="ta-oversight__label">Testing oversight</div>
                        <div v-for="(ph, pi) in testOversightPhases" :key="'test' + pi" class="ta-oversight__phase">
                          <div class="ta-oversight__title">{{ ph.title }}</div>
                          <div class="ta-oversight__summary">{{ ph.summary }}</div>
                          <div v-if="ph.tools_used?.length" class="ta-oversight__tools">
                            <span v-for="t in ph.tools_used" :key="t" class="ta-oversight__tool">{{ t }}</span>
                          </div>
                        </div>
                      </div>
                      <div v-else-if="oversightStatus === 'pending'" class="text-xs text-v-muted">Oversight summary not yet generated.</div>
                      <div v-else-if="oversightStatus === 'generating'" class="text-xs text-v-muted ta-oversight__generating">
                        <span class="spinner" aria-hidden="true"></span>
                        <span class="wf-shimmer-text">Generating oversight summary…</span>
                      </div>
                      <div v-else-if="oversightStatus === 'failed'" class="text-xs" style="color: var(--err, #c0392b);">Oversight generation failed{{ oversightError ? `: ${oversightError}` : '' }}</div>

                      <!-- Pretty activity rows (thinking / tool calls / results / text). -->
                      <div v-if="activity.length">
                        <h3 class="section-title" style="margin-top: 18px;">Transcript</h3>
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
                        <div class="ta-activity-log" :class="{ 'ta-activity-log--streaming': streaming }">
                          <div
                            v-for="(row, i) in visibleActivity"
                            :key="i"
                            class="ta-activity-row"
                            :class="'ta-activity-row--' + row.kind"
                          >
                            <span class="ta-activity-icon" aria-hidden="true">{{ activityIcon(row.kind) }}</span>
                            <span class="ta-activity-label">{{ row.label }}</span>
                            <span v-if="row.summary" class="ta-activity-summary">{{ row.summary }}</span>
                            <span v-if="row.preview" class="ta-activity-preview">{{ row.preview }}</span>
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
                        <pre v-if="diffHighlights[fi]" class="diff-block diff-block-modal"><template v-for="(ln, li) in diffHighlights[fi]!" :key="li"><span class="diff-line" :class="lineClass(ln.kind)"><template v-if="ln.kind === 'header' || ln.kind === 'hunk'">{{ f.lines[li].text }}</template><template v-else>{{ ln.prefix }}<span v-html="ln.html"></span></template></span>
</template></pre>
                        <pre v-else class="diff-block diff-block-modal"><template v-for="(ln, li) in f.lines" :key="li"><span class="diff-line" :class="lineClass(ln.kind)">{{ ln.text }}</span>
</template></pre>
                      </details>
                    </template>
                  </template>
                </div>

                <!-- EVENTS tab -->
                <div data-main-tab-section="events" class="ta-events">
                  <section class="ta-events__sec">
                    <h3 class="ta-events__h">Events</h3>
                    <div v-if="eventsLoading" class="text-xs text-v-muted">Loading events…</div>
                    <div v-else-if="!visibleEvents.length" class="text-xs text-v-muted">No events recorded.</div>
                    <ul v-else class="event-list">
                      <li
                        v-for="g in groupedEvents"
                        :key="g.ref.id"
                        class="event-row"
                        :data-event-type="g.ref.event_type"
                      >
                        <span class="event-row__type">{{ g.ref.event_type }}</span>
                        <span class="event-row__summary">
                          {{ g.summary }}
                          <span v-if="g.count > 1" class="event-row__count">×{{ g.count }}</span>
                        </span>
                        <span class="event-row__time">{{ timeStr(g.ref.created_at) }}</span>
                      </li>
                    </ul>
                  </section>

                  <section class="ta-events__sec">
                    <h3 class="ta-events__h">Usage</h3>
                    <div class="ta-stat-grid">
                      <div class="ta-stat">
                        <span class="ta-stat__label">Input tokens</span>
                        <span class="ta-stat__value">{{ tokenCount(task.usage?.input_tokens) }}</span>
                      </div>
                      <div class="ta-stat">
                        <span class="ta-stat__label">Output tokens</span>
                        <span class="ta-stat__value">{{ tokenCount(task.usage?.output_tokens) }}</span>
                      </div>
                      <div class="ta-stat">
                        <span class="ta-stat__label">Cache read</span>
                        <span class="ta-stat__value">{{ tokenCount(task.usage?.cache_read_input_tokens) }}</span>
                      </div>
                      <div class="ta-stat">
                        <span class="ta-stat__label">Cache creation</span>
                        <span class="ta-stat__value">{{ tokenCount(task.usage?.cache_creation_input_tokens) }}</span>
                      </div>
                      <div class="ta-stat ta-stat--total">
                        <span class="ta-stat__label">Total cost</span>
                        <span class="ta-stat__value">{{ costDisplay }}</span>
                      </div>
                    </div>
                  </section>

                  <section class="ta-events__sec">
                    <h3 class="ta-events__h">Timeline</h3>
                    <div class="ta-stat-grid">
                      <div class="ta-stat">
                        <span class="ta-stat__label">Created</span>
                        <span class="ta-stat__value">{{ timeStr(task.created_at) }}</span>
                      </div>
                      <div class="ta-stat">
                        <span class="ta-stat__label">Updated</span>
                        <span class="ta-stat__value">{{ timeStr(task.updated_at) }}</span>
                      </div>
                      <div class="ta-stat">
                        <span class="ta-stat__label">Turns</span>
                        <span class="ta-stat__value">{{ task.turns ?? 0 }}</span>
                      </div>
                    </div>
                  </section>

                  <!-- Per-sub-agent usage breakdown -->
                  <section v-if="usageBreakdown.length" class="ta-events__sec">
                    <h3 class="ta-events__h">Usage by agent</h3>
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
                  </section>

                  <!-- Retry history (past retired attempts) -->
                  <section v-if="retryHistory.length" class="ta-events__sec">
                    <h3 class="ta-events__h">Retry history</h3>
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
                  </section>

                  <!-- Prompt history (prior iterations) -->
                  <section v-if="promptHistory.length" class="ta-events__sec">
                    <h3 class="ta-events__h">Prompt history</h3>
                    <details v-for="(p, i) in promptHistory" :key="i" class="prompt-record">
                      <summary>#{{ i + 1 }}</summary>
                      <pre>{{ p }}</pre>
                    </details>
                  </section>
                </div>

                <!-- RESULTS tab (multi-turn) -->
                <!-- Verification: the test/verify-phase transcript only. The
                     implementation turns live in Activity (full transcript) and
                     the latest result shows in Spec, so they're not repeated
                     here. -->
                <div data-main-tab-section="verification">
                  <div v-if="resultsLoading" class="text-xs text-v-secondary">Loading verification…</div>
                  <div v-else-if="resultsError" class="text-xs" style="color: var(--err, #c0392b);">{{ resultsError }}</div>
                  <div v-else-if="!testResults.length" class="text-xs text-v-muted">No verification run for this task yet.</div>
                  <template v-else>
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
                </div>

                <!-- TIMELINE tab (span flamegraph) -->
                <div data-main-tab-section="timeline">
                  <h3 class="section-title">Timeline</h3>
                  <div v-if="spansLoading" class="text-xs text-v-secondary">Loading spans…</div>
                  <div v-else-if="spansError" class="text-xs" style="color: var(--err, #c0392b);">{{ spansError }}</div>
                  <SpanFlamegraph v-else :spans="spans" :turn-usages="turnUsages" />
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

                <div v-if="isBacklog" class="aside-action-group">
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
                      <span class="aside-action__label">Edit task</span>
                      <span class="aside-action__hint">prompt, deps, schedule, budget</span>
                    </span>
                  </button>
                  <div v-if="editingBacklog" class="backlog-edit">
                    <div class="backlog-edit__field">
                      <div class="backlog-edit__prompt-tabs">
                        <span>Prompt</span>
                        <button type="button" :class="{ active: !editPromptPreview }" @click="editPromptPreview = false">Edit</button>
                        <button type="button" :class="{ active: editPromptPreview }" @click="editPromptPreview = true">Preview</button>
                      </div>
                      <textarea v-if="!editPromptPreview" v-model="editPrompt" class="backlog-edit__prompt" rows="6" placeholder="Task prompt (Markdown)"></textarea>
                      <!-- eslint-disable-next-line vue/no-v-html — renderMarkdown sanitises -->
                      <div v-else class="backlog-edit__preview prose-content" v-html="editPromptHtml"></div>
                    </div>
                    <label class="backlog-edit__field">
                      <span>Timeout (min)</span>
                      <input v-model.number="editTimeout" type="number" min="1" placeholder="15" />
                    </label>
                    <label class="backlog-edit__field">
                      <span>Model</span>
                      <input v-model="editModel" type="text" placeholder="override model" />
                    </label>
                    <label class="backlog-edit__field">
                      <span>Harness</span>
                      <AppSelect v-model="editSandbox" :options="EDIT_SANDBOX_OPTIONS" aria-label="Harness" block />
                    </label>
                    <div class="backlog-edit__field">
                      <span>Depends on</span>
                      <DependencyPicker v-model="editDeps" :exclude-id="props.task.id" />
                    </div>
                    <label class="backlog-edit__field">
                      <span>Scheduled</span>
                      <input v-model="editScheduledAt" type="datetime-local" />
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

                <div v-if="isWaiting" class="aside-action-group">
                  <button type="button" class="aside-action aside-action--success" @click="completeTask">
                    <span class="aside-action__icon" aria-hidden="true">&#10003;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Mark as Done</span>
                      <span class="aside-action__hint">commit and close</span>
                    </span>
                  </button>
                </div>

                <div v-if="isFailed && task.session_id" class="aside-action-group">
                  <button type="button" class="aside-action" @click="resumeTask">
                    <span class="aside-action__icon" aria-hidden="true">&#8635;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Resume</span>
                      <span class="aside-action__hint">continue existing session</span>
                    </span>
                  </button>
                </div>

                <div v-if="isWaiting || isDone || isFailed" class="aside-action-group">
                  <button type="button" class="aside-action" @click="testTask">
                    <span class="aside-action__icon" aria-hidden="true">&#9654;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Test</span>
                      <span class="aside-action__hint">run test verification</span>
                    </span>
                  </button>
                </div>

                <div v-if="budgetExceeded && (isWaiting || isFailed)" class="aside-action-group">
                  <button type="button" class="aside-action" @click="raiseBudget">
                    <span class="aside-action__icon" aria-hidden="true">&#36;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Raise budget</span>
                      <span class="aside-action__hint">unblock and resume</span>
                    </span>
                  </button>
                </div>

                <div v-if="isWaiting || isFailed" class="aside-action-group">
                  <button type="button" class="aside-action" @click="syncTask">
                    <span class="aside-action__icon" aria-hidden="true">&#8645;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Sync</span>
                      <span class="aside-action__hint">rebase onto default branch</span>
                    </span>
                  </button>
                </div>

                <div v-if="isFailed || isCancelled" class="aside-action-group">
                  <button type="button" class="aside-action" @click="retryTask">
                    <span class="aside-action__icon" aria-hidden="true">&#8634;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Retry</span>
                      <span class="aside-action__hint">move back to Backlog</span>
                    </span>
                  </button>
                </div>

                <div v-if="(isDone || isCancelled) && !isArchived" class="aside-action-group">
                  <button type="button" class="aside-action" @click="archiveTask">
                    <span class="aside-action__icon" aria-hidden="true">&#128229;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Archive</span>
                      <span class="aside-action__hint">hide from board</span>
                    </span>
                  </button>
                </div>

                <div v-if="isArchived" class="aside-action-group">
                  <button type="button" class="aside-action" @click="unarchiveTask">
                    <span class="aside-action__icon" aria-hidden="true">&#128228;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Unarchive</span>
                      <span class="aside-action__hint">restore to board</span>
                    </span>
                  </button>
                </div>

                <div v-if="isInProgress || isWaiting" class="aside-action-group">
                  <button
                    type="button"
                    class="aside-action aside-action--warn"
                    :disabled="cancelling"
                    @click="cancelTask"
                  >
                    <span class="aside-action__icon" aria-hidden="true">{{ cancelling ? '…' : '⏹' }}</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">{{ cancelling ? 'Shutting down…' : 'Cancel' }}</span>
                      <span class="aside-action__hint">{{ cancelling ? 'stopping process' : 'discard changes' }}</span>
                    </span>
                  </button>
                </div>

                <div class="aside-action-group">
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
                  <span class="k">harness</span>
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
                  <span class="k">timeout</span>
                  <span class="v">{{ task.timeout ? task.timeout + ' min' : '—' }}</span>
                </div>
                <div v-if="budgetPct > 0" class="bar" style="margin-top: 6px">
                  <i :style="{ width: budgetPct.toFixed(1) + '%' }"></i>
                </div>
              </div>

              <div class="mdl-section">
                <div class="mdl-h">Git</div>
                <div class="row">
                  <span class="k">branches</span>
                  <span class="v">{{ gitBranches }}</span>
                </div>
                <div class="row">
                  <span class="k">worktrees</span>
                  <span class="v">{{ gitWorktrees }}</span>
                </div>
              </div>

              <div class="mdl-section">
                <div class="mdl-h">Links</div>
                <div class="row">
                  <span class="k">spec</span>
                  <span class="v">
                    <a v-if="specSourcePath" href="#" :title="specSourcePath" @click.prevent="openSpec">{{ specSourceLabel }}</a>
                    <template v-else>—</template>
                  </span>
                </div>
                <div class="row">
                  <span class="k">depends on</span>
                  <span class="v">{{ dependsOnDisplay }}</span>
                </div>
              </div>

              <div v-if="envRows.length" class="mdl-section">
                <div class="mdl-h">Environment</div>
                <dl class="env-provenance">
                  <template v-for="row in envRows" :key="row.label">
                    <dt>{{ row.label }}</dt>
                    <dd :class="{ 'env-provenance__mono': row.mono }">{{ row.value }}</dd>
                  </template>
                </dl>
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
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 10px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--text-secondary);
  margin-bottom: 8px;
}
.ta-oversight__label::before {
  content: '';
  width: 3px;
  height: 11px;
  border-radius: 2px;
  background: color-mix(in oklab, var(--accent) 70%, transparent);
  flex-shrink: 0;
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
  box-sizing: border-box;
  flex: 1;
  width: 100%;
  background: var(--bg-input);
  border: 1px solid var(--border);
  color: var(--text);
  border-radius: var(--r-md, 6px);
  padding: 7px 12px;
  font-size: 13px;
  line-height: 1.4;
  outline: none;
  transition:
    border-color 0.12s,
    box-shadow 0.12s,
    background 0.12s;
}
.ta-activity-search__input::placeholder {
  color: var(--text-muted);
}
.ta-activity-search__input:hover {
  border-color: color-mix(in oklab, var(--accent) 30%, var(--border));
}
.ta-activity-search__input:focus,
.ta-activity-search__input:focus-visible {
  outline: none;
  border-color: var(--accent);
  box-shadow: 0 0 0 3px color-mix(in oklab, var(--accent) 16%, transparent);
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

/* Spec/Result markdown section header with toggle + copy actions. */
.md-section-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.md-section-actions { display: inline-flex; gap: 6px; }

/* Execution-environment provenance list in the right aside. */
.env-provenance {
  display: grid;
  grid-template-columns: auto 1fr;
  gap: 3px 10px;
  margin: 0;
  font-size: 11px;
  align-items: baseline;
}
.env-provenance dt {
  color: var(--text-muted);
  font-size: 10px;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  font-weight: 500;
  white-space: nowrap;
}
.env-provenance dd {
  margin: 0;
  min-width: 0;
  color: var(--text-secondary);
  overflow-wrap: anywhere;
}
.env-provenance__mono { font-family: var(--font-mono, monospace); font-size: 10px; }

/* Dissolve the per-state action wrappers so each button is a direct flex
   child of .modal-aside__actions and inherits its 6px gap (matches old UI). */
.aside-action-group { display: contents; }

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
.backlog-edit__field input,
.backlog-edit__field :deep(.app-select__trigger),
.backlog-edit__prompt {
  background: var(--bg-input);
  border: 1px solid var(--border);
  color: var(--text);
  border-radius: 6px;
  padding: 4px 8px;
  font-size: 12px;
  font-family: var(--font-sans);
}
.backlog-edit__prompt { resize: vertical; line-height: 1.5; }
.backlog-edit__prompt-tabs {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--text-muted);
}
.backlog-edit__prompt-tabs span { margin-right: auto; }
.backlog-edit__prompt-tabs button {
  background: none;
  border: none;
  color: var(--text-muted);
  cursor: pointer;
  font-size: 11px;
  padding: 1px 6px;
  border-radius: 4px;
}
.backlog-edit__prompt-tabs button.active { color: var(--accent); background: rgba(217, 119, 87, 0.1); }
.backlog-edit__preview {
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 6px 8px;
  font-size: 12px;
  max-height: 240px;
  overflow-y: auto;
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
.usage-breakdown { width: 100%; border-collapse: collapse; font-size: 12px; }
.usage-breakdown th {
  text-align: right; padding: 5px 10px; color: var(--text-muted);
  font-weight: 600; text-transform: uppercase; letter-spacing: 0.03em; font-size: 10px;
  border-bottom: 1px solid var(--border);
}
.usage-breakdown th:first-child, .usage-breakdown td:first-child { text-align: left; }
.usage-breakdown td {
  padding: 5px 10px; text-align: right; font-variant-numeric: tabular-nums;
  border-bottom: 1px solid color-mix(in oklab, var(--border) 50%, transparent);
}
.usage-breakdown tbody tr:last-child td { border-bottom: none; }

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

/* --- Events tab: readable sectioned layout --- */
/* Each section is separated by a hairline rule for clear visual grouping. */
.ta-events__sec { padding: 14px 0; border-top: 1px solid var(--border); }
.ta-events__sec:first-child { padding-top: 0; border-top: none; }
.ta-events__h {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text);
  margin: 0 0 10px;
}
.ta-events__h::before {
  content: '';
  width: 3px;
  height: 12px;
  border-radius: 2px;
  background: var(--accent);
  flex-shrink: 0;
}

/* Event timeline rows: taller rows + a colour-coded type pill. */
.event-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 1px; }
.event-row {
  display: grid;
  grid-template-columns: 116px 1fr auto;
  align-items: center;
  gap: 12px;
  padding: 6px 8px 6px 10px;
  font-size: 12px;
  /* A type-coloured left rail makes the stream scannable at a glance. */
  border-left: 2px solid var(--border);
  border-radius: 0 var(--r-md, 6px) var(--r-md, 6px) 0;
}
.event-row[data-event-type="state_change"] { border-left-color: color-mix(in oklab, var(--accent) 55%, transparent); }
.event-row[data-event-type="error"] { border-left-color: var(--err, #c0392b); }
.event-row[data-event-type="output"] { border-left-color: color-mix(in oklab, var(--ok, #3f7a4a) 55%, transparent); }
.event-row:hover { background: var(--bg-hover); }
.event-row__type {
  justify-self: start;
  font-family: var(--font-mono);
  font-size: 10px;
  font-weight: 600;
  color: var(--text-secondary);
  text-transform: uppercase;
  letter-spacing: 0.03em;
  padding: 2px 7px;
  border-radius: 4px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  white-space: nowrap;
}
.event-row[data-event-type="error"] .event-row__type {
  color: var(--err, #c0392b);
  border-color: color-mix(in oklab, var(--err, #c0392b) 40%, var(--border));
  background: color-mix(in oklab, var(--err, #c0392b) 8%, transparent);
}
.event-row[data-event-type="state_change"] .event-row__type {
  color: var(--accent);
  border-color: color-mix(in oklab, var(--accent) 40%, var(--border));
  background: color-mix(in oklab, var(--accent) 8%, transparent);
}
.event-row__summary {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text);
}
.event-row__count {
  margin-left: 6px;
  padding: 0 6px;
  border-radius: 999px;
  background: var(--bg-sunk);
  border: 1px solid var(--border);
  color: var(--text-secondary);
  font-size: 10px;
  font-variant-numeric: tabular-nums;
}
.event-row__time {
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
  text-align: right;
}

/* Usage / Timeline stat grids: roomy two-column key/value rows. */
.ta-stat-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 6px 28px;
}
.ta-stat {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  gap: 12px;
  font-size: 13px;
  padding: 2px 0;
}
.ta-stat__label { color: var(--text-muted); }
.ta-stat__value {
  color: var(--text);
  font-family: var(--font-mono, "SF Mono", monospace);
  font-variant-numeric: tabular-nums;
  text-align: right;
}
.ta-stat--total {
  grid-column: span 2;
  margin-top: 6px;
  padding-top: 8px;
  border-top: 1px solid var(--border);
  font-weight: 600;
}
.ta-stat--total .ta-stat__label { color: var(--text); font-weight: 600; }

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
  padding: 2px 4px 2px 8px;
  /* A kind-colored left rail gives the otherwise-flat monospace stream a
     scannable structure: tool calls, results and thinking each read as their
     own track down the left edge. */
  border-left: 2px solid transparent;
  border-radius: 0 4px 4px 0;
}
.ta-activity-row:hover { background: var(--bg-hover); }

/* While the task is running, rows ease in instead of popping, and the most
   recent one shimmers so it's clear new work is still arriving. Uses the shared
   wf-* keyframes (styles/animations.css). */
.ta-activity-log--streaming .ta-activity-row {
  animation: wf-content-in 240ms ease-out both;
}
.ta-activity-log--streaming .ta-activity-row:last-child .ta-activity-summary {
  background: linear-gradient(
    90deg,
    var(--ink-4) 0%,
    var(--ink-4) 38%,
    var(--ink) 50%,
    var(--ink-4) 62%,
    var(--ink-4) 100%
  );
  background-size: 220% 100%;
  -webkit-background-clip: text;
  background-clip: text;
  -webkit-text-fill-color: transparent;
  color: transparent;
  animation: wf-text-shimmer 1.4s linear infinite;
}
@media (prefers-reduced-motion: reduce) {
  .ta-activity-log--streaming .ta-activity-row { animation: none; }
  .ta-activity-log--streaming .ta-activity-row:last-child .ta-activity-summary {
    animation: none;
    background: none;
    -webkit-text-fill-color: currentColor;
    color: inherit;
  }
}

.ta-oversight__generating {
  display: flex;
  align-items: center;
  gap: 6px;
}
.ta-oversight__generating .spinner {
  width: 12px;
  height: 12px;
  border-width: 1.5px;
}

.ta-activity-icon { text-align: center; opacity: 0.8; }
.ta-activity-label { font-weight: 600; color: var(--text); }
.ta-activity-row--tool { border-left-color: color-mix(in oklab, var(--accent) 55%, transparent); }
.ta-activity-row--tool .ta-activity-label { color: var(--accent); }
.ta-activity-row--tool_result { border-left-color: color-mix(in oklab, var(--ok) 45%, transparent); }
.ta-activity-row--tool_result .ta-activity-label { color: var(--ok); }
.ta-activity-row--thinking { border-left-color: color-mix(in oklab, var(--warn) 45%, transparent); }
.ta-activity-row--thinking .ta-activity-label { color: var(--warn); font-style: italic; }
.ta-activity-summary {
  color: var(--text-muted);
  font-size: 11px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ta-activity-preview {
  grid-column: 2 / -1;
  font-family: var(--font-mono);
  font-size: 11px;
  color: var(--ink-4);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  min-width: 0;
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
