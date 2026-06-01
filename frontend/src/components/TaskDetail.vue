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
import SpanFlamegraph from './SpanFlamegraph.vue';
import type { SpanResult } from '../lib/flamegraph';

const props = defineProps<{ task: Task }>();
const emit = defineEmits<{ close: [] }>();
const dialog = useDialogStore();
const toast = useToastStore();

type MainTab = 'spec' | 'activity' | 'changes' | 'events' | 'timeline';
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

watch(mainTab, (t) => {
  if (t === 'changes' && !diffFetched.value) fetchDiff();
  if (t === 'activity') fetchOversight();
  if (t === 'timeline' && !spansFetched.value) fetchSpans();
});

// Refetch spans when navigating to a different task while the timeline
// tab stays selected (e.g. via deep-link or sidebar nav).
watch(
  () => props.task?.id,
  () => { spansFetched.value = false; spans.value = []; if (mainTab.value === 'timeline') fetchSpans(); },
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
async function cancelTask() {
  const ok = await dialog.confirm({
    title: 'Cancel task',
    message: 'Cancel this task? Running work is stopped and uncommitted worktree changes are discarded.',
    confirmLabel: 'Cancel task',
    cancelLabel: 'Keep',
    danger: true,
  });
  if (!ok) return;
  await api('POST', `/api/tasks/${props.task.id}/cancel`);
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
  await api('POST', `/api/tasks/${props.task.id}/test`);
}
async function syncTask() {
  await api('POST', `/api/tasks/${props.task.id}/sync`);
}
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
    <div id="modal" class="modal-card modal-wide" :data-main-tab="mainTab">
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
          <div id="main-tabs" class="main-tabs" role="tablist">
            <button
              type="button"
              class="main-tab"
              :class="{ active: mainTab === 'spec' }"
              @click="mainTab = 'spec'"
            >Spec</button>
            <button
              type="button"
              class="main-tab"
              :class="{ active: mainTab === 'activity' }"
              @click="mainTab = 'activity'"
            >
              Activity
              <span v-if="streaming" class="pulse-dot" />
            </button>
            <button
              type="button"
              class="main-tab"
              :class="{ active: mainTab === 'changes' }"
              @click="mainTab = 'changes'"
            >Changes</button>
            <button
              type="button"
              class="main-tab"
              :class="{ active: mainTab === 'events' }"
              @click="mainTab = 'events'"
            >Events</button>
            <button
              type="button"
              class="main-tab"
              :class="{ active: mainTab === 'timeline' }"
              @click="mainTab = 'timeline'"
            >Timeline</button>
          </div>

          <div id="modal-row">
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
                      <div v-if="activity.length" class="ta-activity-log">
                        <div
                          v-for="(row, i) in activity"
                          :key="i"
                          class="ta-activity-row"
                          :class="'ta-activity-row--' + row.kind"
                        >
                          <span class="ta-activity-icon" aria-hidden="true">{{ activityIcon(row.kind) }}</span>
                          <span class="ta-activity-label">{{ row.label }}</span>
                          <span v-if="row.summary" class="ta-activity-summary">{{ row.summary }}</span>
                          <details v-if="row.detail" class="ta-activity-detail" :open="row.defaultOpen">
                            <summary>details</summary>
                            <pre>{{ row.detail }}</pre>
                          </details>
                        </div>
                      </div>

                      <!-- Fallback: raw output when nothing parsed into activity. -->
                      <div v-else class="activity-oversight-box" id="modal-logs-section">
                        <pre ref="logContainer" class="logs-block"><span v-if="!rawOutput" class="cc-result-empty">{{ streaming ? 'Connecting…' : 'No output' }}</span>{{ rawOutput }}</pre>
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
                  <button type="button" class="aside-action aside-action--warn" @click="cancelTask">
                    <span class="aside-action__icon" aria-hidden="true">&#9209;</span>
                    <span class="aside-action__body">
                      <span class="aside-action__label">Cancel</span>
                      <span class="aside-action__hint">discard changes</span>
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
