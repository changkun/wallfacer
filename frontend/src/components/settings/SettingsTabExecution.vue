<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { api } from '../../api/client';
import { useEnvConfig } from '../../composables/useEnvConfig';
import { useTaskStore } from '../../stores/tasks';
import { useAutomationToggles } from '../../composables/useAutomationToggles';
import AppSelect from '../AppSelect.vue';
import SettingToggle from './SettingToggle.vue';

const LIMIT_OPTIONS = [
  { value: '5', label: '5 tasks' },
  { value: '10', label: '10 tasks' },
  { value: '25', label: '25 tasks' },
  { value: '50', label: '50 tasks' },
  { value: '0', label: 'All' },
];

const taskStore = useTaskStore();

// Automation toggles share one source of truth with the board AutomationMenu
// popover via useAutomationToggles (server config, not the shell env).
const {
  AUTOMATION_KEYS,
  automationLabels,
  automationHints,
  isOn: automationOn,
  isBusy: automationBusy,
  toggle: toggleAutomation,
} = useAutomationToggles();

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
  if (!taskStore.config) await taskStore.fetchConfig();
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
  <div class="card compact" data-settings-tab="execution">
    <div class="card-head"><span class="eyebrow">Automation</span></div>
    <div class="rows">
      <div v-for="k in AUTOMATION_KEYS" :key="k" class="set-row">
        <div class="set-row__main">
          <span class="set-row__label">{{ automationLabels[k] }}</span>
          <span class="set-row__help">{{ automationHints[k] }}<template v-if="automationBusy(k)"> · saving…</template></span>
        </div>
        <div class="set-row__end">
          <SettingToggle :model-value="automationOn(k)" :disabled="automationBusy(k)" :label="automationLabels[k]" @update:model-value="toggleAutomation(k)" />
        </div>
      </div>
    </div>
    <div class="card-foot"><span class="set-row__help">Each toggle drives one server-side watcher; see the automation guide for the full lifecycle and budgets.</span></div>
  </div>

  <div class="card compact">
    <div class="card-head"><span class="eyebrow">Limits</span></div>
    <div class="rows">
      <div class="set-row">
        <div class="set-row__main">
          <span class="set-row__label">Parallel tasks</span>
          <span class="set-row__help">Max tasks running concurrently in the In Progress column.</span>
        </div>
        <div class="set-row__end">
          <span id="max-parallel-status" class="set-status">{{ maxParallelStatus }}</span>
          <input id="max-parallel-input" v-model.number="maxParallel" type="number" min="1" max="20" class="field set-num" autocomplete="off" @change="saveMaxParallel" />
        </div>
      </div>
      <div class="set-row">
        <div class="set-row__main">
          <span class="set-row__label">Archived tasks per page</span>
          <span class="set-row__help">When archived tasks are visible, load this many items per scroll page.</span>
        </div>
        <div class="set-row__end">
          <span id="archived-page-size-status" class="set-status">{{ archivedStatus }}</span>
          <input id="archived-page-size-input" v-model.number="archivedPerPage" type="number" min="1" max="200" class="field set-num" autocomplete="off" @change="saveArchivedPerPage" />
        </div>
      </div>
      <div class="set-row">
        <div class="set-row__main">
          <span class="set-row__label">Oversight interval</span>
          <span class="set-row__help">Generate oversight summaries every N minutes while a task runs. 0 = only at task completion.</span>
        </div>
        <div class="set-row__end">
          <span id="oversight-interval-status" class="set-status">{{ oversightStatus }}</span>
          <input id="oversight-interval-input" v-model.number="oversightInterval" type="number" min="0" max="120" class="field set-num" autocomplete="off" @change="saveOversightInterval" />
          <span class="set-unit">min</span>
        </div>
      </div>
    </div>
  </div>

  <div class="card compact">
    <div class="card-head"><span class="eyebrow">Auto push</span></div>
    <div class="rows">
      <div class="set-row">
        <div class="set-row__main">
          <span class="set-row__label">Push after the commit pipeline</span>
          <span class="set-row__help">Automatically push when the workspace is at least N commits ahead of upstream.</span>
        </div>
        <div class="set-row__end">
          <span id="auto-push-status" class="set-status">{{ autoPushStatus }}</span>
          <SettingToggle id="auto-push-enabled" v-model="autoPushEnabled" label="Auto push" @update:model-value="saveAutoPush" />
        </div>
      </div>
      <div v-show="showThresholdRow" id="auto-push-threshold-row" class="set-row">
        <div class="set-row__main">
          <span class="set-row__label">Push when ahead by</span>
        </div>
        <div class="set-row__end">
          <input id="auto-push-threshold" v-model.number="autoPushThreshold" type="number" min="1" class="field set-num" autocomplete="off" @change="saveAutoPush" />
          <span class="set-unit">commit(s)</span>
        </div>
      </div>
    </div>
  </div>

  <div class="card compact">
    <div class="card-head"><span class="eyebrow">Maintenance</span></div>
    <div class="rows">
      <div class="set-row">
        <div class="set-row__main">
          <span class="set-row__label">Task titles</span>
          <span id="generate-titles-status" class="set-row__help">{{ titlesStatus || 'Generate a title for tasks that have none.' }}</span>
        </div>
        <div class="set-row__end">
          <AppSelect v-model="titlesLimit" :options="LIMIT_OPTIONS" aria-label="Max tasks to generate titles for" title="Max tasks to generate titles for" />
          <button type="button" class="btn sm ghost" :disabled="titlesBusy" @click="generateMissingTitles">Generate missing</button>
        </div>
      </div>
      <div class="set-row">
        <div class="set-row__main">
          <span class="set-row__label">Trace oversight</span>
          <span id="generate-oversight-status" class="set-row__help">{{ oversightGenStatus || 'Generate oversight summaries for tasks that have none.' }}</span>
        </div>
        <div class="set-row__end">
          <AppSelect v-model="oversightLimit" :options="LIMIT_OPTIONS" aria-label="Max tasks to generate oversight for" title="Max tasks to generate oversight for" />
          <button type="button" class="btn sm ghost" :disabled="oversightGenBusy" @click="generateMissingOversight">Generate missing</button>
        </div>
      </div>
    </div>
  </div>
</template>
