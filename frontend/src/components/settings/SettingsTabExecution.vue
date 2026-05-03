<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { api } from '../../api/client';
import { useEnvConfig } from '../../composables/useEnvConfig';

const { env, fetchEnv, updateEnv } = useEnvConfig();

const maxParallel = ref(5);
const archivedPerPage = ref(20);
const oversightInterval = ref(0);
const autoPushEnabled = ref(false);
const autoPushThreshold = ref(1);

const maxParallelStatus = ref('');
const archivedStatus = ref('');
const oversightStatus = ref('');
const autoPushStatus = ref('');

const titlesLimit = ref('10');
const titlesStatus = ref('');
const titlesBusy = ref(false);

const oversightLimit = ref('10');
const oversightGenStatus = ref('');
const oversightGenBusy = ref(false);

const showThresholdRow = computed(() => autoPushEnabled.value);

function clamp(value: number, min: number, max: number): number {
  if (Number.isNaN(value)) return min;
  if (value < min) return min;
  if (value > max) return max;
  return value;
}

function flashSaved(target: { value: string }) {
  target.value = 'Saved.';
  setTimeout(() => {
    if (target.value === 'Saved.') target.value = '';
  }, 2000);
}

function syncFromEnv() {
  if (!env.value) return;
  if (typeof env.value.max_parallel_tasks === 'number' && env.value.max_parallel_tasks > 0) {
    maxParallel.value = env.value.max_parallel_tasks;
  }
  if (typeof env.value.archived_tasks_per_page === 'number' && env.value.archived_tasks_per_page > 0) {
    archivedPerPage.value = env.value.archived_tasks_per_page;
  }
  if (typeof env.value.oversight_interval === 'number') {
    oversightInterval.value = env.value.oversight_interval;
  }
  autoPushEnabled.value = !!env.value.auto_push_enabled;
  if (typeof env.value.auto_push_threshold === 'number' && env.value.auto_push_threshold > 0) {
    autoPushThreshold.value = env.value.auto_push_threshold;
  }
}

watch(env, syncFromEnv);

onMounted(async () => {
  await fetchEnv();
  syncFromEnv();
});

async function saveMaxParallel() {
  const value = clamp(parseInt(String(maxParallel.value), 10), 1, 20);
  maxParallel.value = value;
  maxParallelStatus.value = 'Saving…';
  try {
    await updateEnv({ max_parallel_tasks: value });
    flashSaved(maxParallelStatus);
  } catch (e) {
    maxParallelStatus.value = 'Error: ' + (e instanceof Error ? e.message : String(e));
  }
}

async function saveArchivedPerPage() {
  const value = clamp(parseInt(String(archivedPerPage.value), 10), 1, 200);
  archivedPerPage.value = value;
  archivedStatus.value = 'Saving…';
  try {
    await updateEnv({ archived_tasks_per_page: value });
    flashSaved(archivedStatus);
  } catch (e) {
    archivedStatus.value = 'Error: ' + (e instanceof Error ? e.message : String(e));
  }
}

async function saveOversightInterval() {
  const value = clamp(parseInt(String(oversightInterval.value), 10), 0, 120);
  oversightInterval.value = value;
  oversightStatus.value = 'Saving…';
  try {
    await updateEnv({ oversight_interval: value });
    flashSaved(oversightStatus);
  } catch (e) {
    oversightStatus.value = 'Error: ' + (e instanceof Error ? e.message : String(e));
  }
}

async function saveAutoPush() {
  let threshold = parseInt(String(autoPushThreshold.value), 10);
  if (Number.isNaN(threshold) || threshold < 1) threshold = 1;
  autoPushThreshold.value = threshold;
  autoPushStatus.value = 'Saving…';
  try {
    await updateEnv({
      auto_push_enabled: autoPushEnabled.value,
      auto_push_threshold: threshold,
    });
    flashSaved(autoPushStatus);
  } catch (e) {
    autoPushStatus.value = 'Error: ' + (e instanceof Error ? e.message : String(e));
  }
}

interface GenerateResponse {
  generated?: number;
  count?: number;
  message?: string;
}

async function generateMissingTitles() {
  if (titlesBusy.value) return;
  titlesBusy.value = true;
  const limit = parseInt(titlesLimit.value, 10) || 0;
  titlesStatus.value = 'Generating titles…';
  try {
    const resp = await api<GenerateResponse>('POST', '/api/tasks/generate-titles', { limit });
    const n = resp?.generated ?? resp?.count ?? 0;
    titlesStatus.value = resp?.message || (n > 0 ? `Generated ${n} title(s).` : 'No tasks needed titles.');
    setTimeout(() => {
      if (titlesStatus.value && !titlesStatus.value.startsWith('Error')) titlesStatus.value = '';
    }, 4000);
  } catch (e) {
    titlesStatus.value = 'Error: ' + (e instanceof Error ? e.message : String(e));
  } finally {
    titlesBusy.value = false;
  }
}

async function generateMissingOversight() {
  if (oversightGenBusy.value) return;
  oversightGenBusy.value = true;
  const limit = parseInt(oversightLimit.value, 10) || 0;
  oversightGenStatus.value = 'Generating oversight…';
  try {
    const resp = await api<GenerateResponse>('POST', '/api/tasks/generate-oversight', { limit });
    const n = resp?.generated ?? resp?.count ?? 0;
    oversightGenStatus.value = resp?.message || (n > 0 ? `Generated ${n} oversight summary(ies).` : 'No tasks needed oversight.');
    setTimeout(() => {
      if (oversightGenStatus.value && !oversightGenStatus.value.startsWith('Error')) oversightGenStatus.value = '';
    }, 4000);
  } catch (e) {
    oversightGenStatus.value = 'Error: ' + (e instanceof Error ? e.message : String(e));
  } finally {
    oversightGenBusy.value = false;
  }
}
</script>

<template>
  <div class="settings-tab-content active" data-settings-tab="execution">
    <div
      style="margin-bottom: 8px; font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px"
    >
      Parallel Tasks
    </div>
    <div style="display: flex; align-items: center; gap: 8px">
      <input
        id="max-parallel-input"
        v-model.number="maxParallel"
        type="number"
        min="1"
        max="20"
        class="field"
        style="width: 60px; font-size: 12px; padding: 3px 6px; text-align: center"
        autocomplete="off"
        @change="saveMaxParallel"
      />
      <span
        id="max-parallel-status"
        style="font-size: 11px; color: var(--text-muted)"
        >{{ maxParallelStatus }}</span
      >
    </div>
    <div
      style="margin-top: 6px; font-size: 11px; color: var(--text-muted); line-height: 1.4"
    >
      Max tasks running concurrently in the In Progress column.
    </div>

    <div class="settings-section">
      <div
        style="margin-bottom: 8px; font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px"
      >
        Archived Tasks
      </div>
      <div style="display: flex; align-items: center; gap: 8px">
        <input
          id="archived-page-size-input"
          v-model.number="archivedPerPage"
          type="number"
          min="1"
          max="200"
          class="field"
          style="width: 60px; font-size: 12px; padding: 3px 6px; text-align: center"
          autocomplete="off"
          @change="saveArchivedPerPage"
        />
        <span style="font-size: 12px; color: var(--text-muted)">per page</span>
        <span
          id="archived-page-size-status"
          style="font-size: 11px; color: var(--text-muted)"
          >{{ archivedStatus }}</span
        >
      </div>
      <div
        style="margin-top: 6px; font-size: 11px; color: var(--text-muted); line-height: 1.4"
      >
        When archived tasks are visible, load this many items per scroll page.
      </div>
    </div>

    <div class="settings-section">
      <div
        style="margin-bottom: 8px; font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px"
      >
        Oversight Interval
      </div>
      <div style="display: flex; align-items: center; gap: 8px">
        <input
          id="oversight-interval-input"
          v-model.number="oversightInterval"
          type="number"
          min="0"
          max="120"
          class="field"
          style="width: 60px; font-size: 12px; padding: 3px 6px; text-align: center"
          autocomplete="off"
          @change="saveOversightInterval"
        />
        <span style="font-size: 12px; color: var(--text-muted)">min</span>
        <span
          id="oversight-interval-status"
          style="font-size: 11px; color: var(--text-muted)"
          >{{ oversightStatus }}</span
        >
      </div>
      <div
        style="margin-top: 6px; font-size: 11px; color: var(--text-muted); line-height: 1.4"
      >
        Generate oversight summaries every N minutes while a task runs. 0 = only
        at task completion.
      </div>
    </div>

    <div class="settings-section">
      <div
        style="margin-bottom: 8px; font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px"
      >
        Auto Push
      </div>
      <div style="display: flex; align-items: center; gap: 8px">
        <label
          style="display: flex; align-items: center; gap: 6px; cursor: pointer; font-size: 12px; color: var(--text-muted)"
        >
          <input
            id="auto-push-enabled"
            v-model="autoPushEnabled"
            type="checkbox"
            @change="saveAutoPush"
          />
          Enable
        </label>
        <span
          id="auto-push-status"
          style="font-size: 11px; color: var(--text-muted)"
          >{{ autoPushStatus }}</span
        >
      </div>
      <div
        v-show="showThresholdRow"
        id="auto-push-threshold-row"
        style="margin-top: 8px; display: flex; align-items: center; gap: 8px"
      >
        <span style="font-size: 12px; color: var(--text-muted)">Push when</span>
        <input
          id="auto-push-threshold"
          v-model.number="autoPushThreshold"
          type="number"
          min="1"
          class="field"
          style="width: 60px; font-size: 12px; padding: 3px 6px; text-align: center"
          autocomplete="off"
          @change="saveAutoPush"
        />
        <span style="font-size: 12px; color: var(--text-muted)"
          >commit(s) ahead</span
        >
      </div>
      <div
        style="margin-top: 6px; font-size: 11px; color: var(--text-muted); line-height: 1.4"
      >
        Automatically push after the commit pipeline when the workspace is at
        least N commits ahead of upstream.
      </div>
    </div>

    <div class="settings-section">
      <div
        style="margin-bottom: 8px; font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px"
      >
        Task Titles
      </div>
      <div style="display: flex; gap: 6px; align-items: center">
        <select
          id="generate-titles-limit"
          v-model="titlesLimit"
          class="select"
          style="font-size: 12px; padding: 3px 6px; height: auto"
          title="Max tasks to generate titles for"
        >
          <option value="5">5 tasks</option>
          <option value="10">10 tasks</option>
          <option value="25">25 tasks</option>
          <option value="50">50 tasks</option>
          <option value="0">All</option>
        </select>
        <button
          type="button"
          class="btn-icon"
          style="font-size: 12px; padding: 4px 12px"
          :disabled="titlesBusy"
          @click="generateMissingTitles"
        >
          Generate Missing
        </button>
      </div>
      <div
        id="generate-titles-status"
        style="margin-top: 6px; font-size: 11px; color: var(--text-muted); line-height: 1.4; min-height: 1em"
      >
        {{ titlesStatus }}
      </div>
    </div>

    <div class="settings-section">
      <div
        style="margin-bottom: 8px; font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px"
      >
        Trace Oversight
      </div>
      <div style="display: flex; gap: 6px; align-items: center">
        <select
          id="generate-oversight-limit"
          v-model="oversightLimit"
          class="select"
          style="font-size: 12px; padding: 3px 6px; height: auto"
          title="Max tasks to generate oversight for"
        >
          <option value="5">5 tasks</option>
          <option value="10">10 tasks</option>
          <option value="25">25 tasks</option>
          <option value="50">50 tasks</option>
          <option value="0">All</option>
        </select>
        <button
          type="button"
          class="btn-icon"
          style="font-size: 12px; padding: 4px 12px"
          :disabled="oversightGenBusy"
          @click="generateMissingOversight"
        >
          Generate Missing
        </button>
      </div>
      <div
        id="generate-oversight-status"
        style="margin-top: 6px; font-size: 11px; color: var(--text-muted); line-height: 1.4; min-height: 1em"
      >
        {{ oversightGenStatus }}
      </div>
    </div>
  </div>
</template>
