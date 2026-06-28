<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { api } from '../../api/client';
import { useEnvConfig } from '../../composables/useEnvConfig';
import { useTaskStore } from '../../stores/tasks';
import { useAutomationToggles } from '../../composables/useAutomationToggles';
import AppSelect from '../AppSelect.vue';

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
const maxAgents = ref(0);
const agentNice = ref(10);
const agonForks = ref(1);
const agonRounds = ref(3);
const agonCostCap = ref(50000);

const maxParallelStatus = ref('');
const archivedStatus = ref('');
const oversightStatus = ref('');
const autoPushStatus = ref('');
const maxAgentsStatus = ref('');
const agentNiceStatus = ref('');
const agonStatus = ref('');

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
  if (typeof env.value.max_agents === 'number') {
    maxAgents.value = env.value.max_agents;
  }
  if (typeof env.value.agent_nice === 'number') {
    agentNice.value = env.value.agent_nice;
  }
  if (typeof env.value.agon_forks === 'number' && env.value.agon_forks > 0) {
    agonForks.value = env.value.agon_forks;
  }
  if (typeof env.value.agon_rounds === 'number' && env.value.agon_rounds > 0) {
    agonRounds.value = env.value.agon_rounds;
  }
  if (typeof env.value.agon_cost_cap === 'number' && env.value.agon_cost_cap > 0) {
    agonCostCap.value = env.value.agon_cost_cap;
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

async function saveMaxAgents() {
  const value = clamp(parseInt(String(maxAgents.value), 10), 0, 64);
  maxAgents.value = value;
  maxAgentsStatus.value = 'Saving…';
  try {
    await updateEnv({ max_agents: value });
    maxAgentsStatus.value = 'Saved — applies on restart.';
    setTimeout(() => {
      if (maxAgentsStatus.value.startsWith('Saved')) maxAgentsStatus.value = '';
    }, 3000);
  } catch (e) {
    maxAgentsStatus.value = 'Error: ' + (e instanceof Error ? e.message : String(e));
  }
}

async function saveAgentNice() {
  const value = clamp(parseInt(String(agentNice.value), 10), -1, 19);
  agentNice.value = value;
  agentNiceStatus.value = 'Saving…';
  try {
    await updateEnv({ agent_nice: value });
    agentNiceStatus.value = 'Saved — applies on restart.';
    setTimeout(() => {
      if (agentNiceStatus.value.startsWith('Saved')) agentNiceStatus.value = '';
    }, 3000);
  } catch (e) {
    agentNiceStatus.value = 'Error: ' + (e instanceof Error ? e.message : String(e));
  }
}

async function saveAgon() {
  const forks = clamp(parseInt(String(agonForks.value), 10), 1, 8);
  const rounds = clamp(parseInt(String(agonRounds.value), 10), 1, 12);
  const costCap = clamp(parseInt(String(agonCostCap.value), 10), 1000, 1000000);
  agonForks.value = forks;
  agonRounds.value = rounds;
  agonCostCap.value = costCap;
  agonStatus.value = 'Saving…';
  try {
    await updateEnv({ agon_forks: forks, agon_rounds: rounds, agon_cost_cap: costCap });
    flashSaved(agonStatus);
  } catch (e) {
    agonStatus.value = 'Error: ' + (e instanceof Error ? e.message : String(e));
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
      Automation
    </div>
    <div class="automation-grid">
      <label
        v-for="k in AUTOMATION_KEYS"
        :key="k"
        class="automation-row"
      >
        <input
          type="checkbox"
          :checked="automationOn(k)"
          :disabled="automationBusy(k)"
          @change="toggleAutomation(k)"
        />
        <span class="automation-label">{{ automationLabels[k] }}</span>
        <span class="automation-hint">— {{ automationHints[k] }}</span>
        <span v-if="automationBusy(k)" class="automation-hint">saving…</span>
      </label>
    </div>
    <div
      style="margin: 4px 0 14px; font-size: 11px; color: var(--text-muted); line-height: 1.4"
    >
      Each toggle drives one server-side watcher; see docs/guide/automation.md
      for the full lifecycle and budgets.
    </div>

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
        Verification &amp; Resources
      </div>

      <div style="display: flex; align-items: center; gap: 8px; flex-wrap: wrap">
        <label style="font-size: 12px; color: var(--text-muted)" for="agon-forks-input"
          >Agon forks</label
        >
        <input
          id="agon-forks-input"
          v-model.number="agonForks"
          type="number"
          min="1"
          max="8"
          class="field"
          style="width: 56px; font-size: 12px; padding: 3px 6px; text-align: center"
          autocomplete="off"
          @change="saveAgon"
        />
        <label style="font-size: 12px; color: var(--text-muted)" for="agon-rounds-input"
          >rounds</label
        >
        <input
          id="agon-rounds-input"
          v-model.number="agonRounds"
          type="number"
          min="1"
          max="12"
          class="field"
          style="width: 56px; font-size: 12px; padding: 3px 6px; text-align: center"
          autocomplete="off"
          @change="saveAgon"
        />
        <label style="font-size: 12px; color: var(--text-muted)" for="agon-cap-input"
          >token cap</label
        >
        <input
          id="agon-cap-input"
          v-model.number="agonCostCap"
          type="number"
          min="1000"
          max="1000000"
          step="1000"
          class="field"
          style="width: 90px; font-size: 12px; padding: 3px 6px; text-align: center"
          autocomplete="off"
          @change="saveAgon"
        />
        <span style="font-size: 11px; color: var(--text-muted)">{{ agonStatus }}</span>
      </div>
      <div
        style="margin-top: 6px; font-size: 11px; color: var(--text-muted); line-height: 1.4"
      >
        Adversarial verification depth. The minimum (1 fork, 3 rounds) is the
        cheapest meaningful debate; raise for more scrutiny at higher cost.
        Applies to the next run.
      </div>

      <div style="display: flex; align-items: center; gap: 8px; margin-top: 12px">
        <label style="font-size: 12px; color: var(--text-muted)" for="agent-nice-input"
          >Agent niceness</label
        >
        <input
          id="agent-nice-input"
          v-model.number="agentNice"
          type="number"
          min="-1"
          max="19"
          class="field"
          style="width: 56px; font-size: 12px; padding: 3px 6px; text-align: center"
          autocomplete="off"
          @change="saveAgentNice"
        />
        <span style="font-size: 11px; color: var(--text-muted)">{{ agentNiceStatus }}</span>
      </div>
      <div
        style="margin-top: 6px; font-size: 11px; color: var(--text-muted); line-height: 1.4"
      >
        OS priority for agent processes so they yield CPU to the foreground.
        Higher = nicer (lower priority); -1 disables throttling.
      </div>

      <div style="display: flex; align-items: center; gap: 8px; margin-top: 12px">
        <label style="font-size: 12px; color: var(--text-muted)" for="max-agents-input"
          >Max concurrent agents</label
        >
        <input
          id="max-agents-input"
          v-model.number="maxAgents"
          type="number"
          min="0"
          max="64"
          class="field"
          style="width: 56px; font-size: 12px; padding: 3px 6px; text-align: center"
          autocomplete="off"
          @change="saveMaxAgents"
        />
        <span style="font-size: 11px; color: var(--text-muted)">{{ maxAgentsStatus }}</span>
      </div>
      <div
        style="margin-top: 6px; font-size: 11px; color: var(--text-muted); line-height: 1.4"
      >
        Hard ceiling on agent processes running at once across tasks, tests, and
        verification. 0 = unlimited.
      </div>
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
        <AppSelect
          v-model="titlesLimit"
          :options="LIMIT_OPTIONS"
          aria-label="Max tasks to generate titles for"
          title="Max tasks to generate titles for"
        />
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
        <AppSelect
          v-model="oversightLimit"
          :options="LIMIT_OPTIONS"
          aria-label="Max tasks to generate oversight for"
          title="Max tasks to generate oversight for"
        />
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

<style scoped>
.automation-grid {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-bottom: 8px;
}
.automation-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--text);
  cursor: pointer;
}
.automation-row input[type="checkbox"] { margin: 0; accent-color: var(--accent); }
.automation-label { flex: 1; }
.automation-hint {
  font-size: 11px;
  color: var(--text-muted);
  font-family: var(--font-mono);
}
</style>
