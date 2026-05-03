<script setup lang="ts">
import { ref, computed, nextTick, watch, onMounted, onUnmounted } from 'vue';
import { api } from '../api/client';
import { useLogStream } from '../composables/useLogStream';
import type { Task } from '../api/types';

const props = defineProps<{ task: Task }>();
const emit = defineEmits<{ close: [] }>();

type MainTab = 'spec' | 'activity' | 'events';
const mainTab = ref<MainTab>('spec');

const feedback = ref('');
const submittingFeedback = ref(false);
const logContainer = ref<HTMLElement | null>(null);

const streamTaskId = computed(() =>
  (props.task.status === 'in_progress' || props.task.status === 'committing') ? props.task.id : null,
);
const { lines, streaming } = useLogStream(streamTaskId);

watch(lines, async () => {
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
  await api('POST', `/api/tasks/${props.task.id}/cancel`);
}
async function retryTask() {
  await api('PATCH', `/api/tasks/${props.task.id}`, { status: 'backlog' });
}
async function completeTask() {
  await api('POST', `/api/tasks/${props.task.id}/done`);
}
async function archiveTask() {
  await api('POST', `/api/tasks/${props.task.id}/archive`);
}
async function unarchiveTask() {
  await api('POST', `/api/tasks/${props.task.id}/unarchive`);
}
async function deleteTask() {
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
onUnmounted(() => document.removeEventListener('keydown', onKeydown));

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
              :class="{ active: mainTab === 'events' }"
              @click="mainTab = 'events'"
            >Events</button>
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
                    <textarea
                      v-model="feedback"
                      rows="3"
                      placeholder="Type your response..."
                      class="field"
                    />
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
                        <span class="activity-block__label">Logs</span>
                        <span v-if="streaming" class="text-xs text-v-muted">streaming…</span>
                      </div>
                      <div class="activity-oversight-box" id="modal-logs-section">
                        <pre ref="logContainer" class="logs-block">
<span v-if="!streaming && lines.length === 0" class="cc-result-empty">{{ isInProgress ? 'Connecting…' : 'No logs (task not running)' }}</span><template v-for="(line, i) in lines" :key="i">{{ line }}
</template></pre>
                      </div>
                    </div>
                  </section>
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
</style>
