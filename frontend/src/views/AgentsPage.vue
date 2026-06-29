<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { useRoute } from 'vue-router';
import { api, ApiError } from '../api/client';
import { useTaskStore } from '../stores/tasks';
import { useUiStore } from '../stores/ui';
import { useEnvConfig } from '../composables/useEnvConfig';
import { supportedHarnesses, harnessLabel } from '../lib/harness';
import { useDialogStore } from '../stores/dialog';
import AppSelect from '../components/AppSelect.vue';
import AgentEditor from '../components/AgentEditor.vue';

interface Agent {
  slug: string;
  title: string;
  description?: string;
  capabilities?: string[];
  multiturn?: boolean;
  harness?: string;
  prompt_template_name?: string;
  builtin: boolean;
  prompt_tmpl?: string;
}

interface Draft {
  slug: string;
  title: string;
  description: string;
  harness: string;
  multiturn: boolean;
  capabilities: string[];
  prompt_tmpl: string;
}

const store = useTaskStore();
const ui = useUiStore();
const { updateEnv } = useEnvConfig();
const dialog = useDialogStore();

const agents = ref<Agent[]>([]);
const loading = ref(true);
const search = ref('');
const selectedSlug = ref<string | null>(null);
// Editor state owned here for the rail: `isNew` toggles the create/clone
// flow, `cloneSeed` is the source row a clone starts from, and `draftModel`
// mirrors the editor's live new-agent draft so the rail pill stays in sync.
const isNew = ref(false);
const cloneSeed = ref<Agent | null>(null);
const draftModel = ref<Draft | null>(null);

const defaultHarness = computed(() => store.config?.default_sandbox || 'claude');

// Supported harnesses advertised by the server (falls back to the full
// registry before config loads), used to populate every harness picker.
const harnessChoices = computed(() => supportedHarnesses(store.config?.sandboxes));

const harnessSelectOptions = computed(() =>
  harnessChoices.value.map((id) => ({ value: id, label: harnessLabel(id) })),
);

const savingDefault = ref(false);
async function setDefaultHarness(value: string) {
  if (value === defaultHarness.value) return;
  savingDefault.value = true;
  try {
    await updateEnv({ default_sandbox: value });
    await store.fetchConfig();
  } catch (err) {
    await dialog.alert(
      'Could not change default harness: ' +
        (err instanceof ApiError ? err.message : String(err)),
    );
  } finally {
    savingDefault.value = false;
  }
}

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase();
  if (!q) return agents.value.slice();
  return agents.value.filter((a) => {
    return (
      (a.slug || '').toLowerCase().includes(q) ||
      (a.title || '').toLowerCase().includes(q) ||
      (a.description || '').toLowerCase().includes(q)
    );
  });
});
const builtins = computed(() => filtered.value.filter((a) => a.builtin));
const userAgents = computed(() => filtered.value.filter((a) => !a.builtin));

// The agent shown in the editor: the clone source while creating, otherwise
// the selected row. AgentEditor owns the form state and CRUD calls.
const editorAgent = computed(() => {
  if (isNew.value) return cloneSeed.value;
  if (!selectedSlug.value) return null;
  return agents.value.find((a) => a.slug === selectedSlug.value) || null;
});
const hasEditor = computed(() => isNew.value || !!editorAgent.value);

async function loadAgents() {
  loading.value = true;
  try {
    const rows = await api<Agent[]>('GET', '/api/agents');
    agents.value = Array.isArray(rows) ? rows : [];
    if (selectedSlug.value && !isNew.value && !agents.value.find((a) => a.slug === selectedSlug.value)) {
      selectedSlug.value = null;
    }
  } catch (e) {
    console.error('agents:', e);
  } finally {
    loading.value = false;
  }
}

function selectAgent(a: Agent) {
  isNew.value = false;
  cloneSeed.value = null;
  selectedSlug.value = a.slug;
}

function openNewEditor() {
  cloneSeed.value = null;
  isNew.value = true;
  selectedSlug.value = null;
}

function startClone(role: Agent) {
  cloneSeed.value = role;
  isNew.value = true;
  selectedSlug.value = null;
}

async function onSaved(slug: string) {
  isNew.value = false;
  cloneSeed.value = null;
  selectedSlug.value = slug;
  await loadAgents();
  const a = agents.value.find((x) => x.slug === slug);
  selectedSlug.value = a ? a.slug : null;
}

async function onDeleted(_slug: string) {
  isNew.value = false;
  cloneSeed.value = null;
  selectedSlug.value = null;
  await loadAgents();
}

function onCancel() {
  isNew.value = false;
  cloneSeed.value = null;
}

function openSandboxSettings() {
  // Settings is at /settings; the Sandbox tab is reachable via hash.
  window.location.href = '/settings#sandbox';
}

const route = useRoute();

onMounted(async () => {
  if (!store.config) await store.fetchConfig();
  await loadAgents();
  // Deep link from the agent-graph: ?agent=<slug> opens that agent's editor.
  const want = typeof route.query.agent === 'string' ? route.query.agent : '';
  if (want) {
    const a = agents.value.find((x) => x.slug === want);
    if (a) selectAgent(a);
  }
});
</script>

<template>
  <div class="agents-mode-container">
    <div class="agents-mode__inner">
      <header class="agents-mode__header">
        <div class="agents-mode__header-row">
          <div>
            <h2 class="agents-mode__title">Agents</h2>
            <p class="agents-mode__subtitle">
              Sub-agent roles each flow step invokes. Clone a built-in or start
              from scratch to pin a harness, tune capabilities, or override the
              system prompt.
            </p>
          </div>
          <div class="agents-mode__header-actions">
            <button
              type="button"
              class="agents-mode__secondary"
              title="Edit the built-in system prompt templates agents start from"
              @click="ui.openSystemPrompts()"
            >
              System Prompts
            </button>
            <button type="button" class="agents-mode__new" @click="openNewEditor">
              + New Agent
            </button>
          </div>
        </div>
        <div class="agents-mode__default-row">
          <span class="agents-mode__default-label">Workspace default harness</span>
          <AppSelect
            class="agents-mode__default-select"
            :model-value="defaultHarness"
            :options="harnessSelectOptions"
            :disabled="savingDefault"
            aria-label="Workspace default harness"
            title="Pick the harness new tasks use by default"
            @update:model-value="setDefaultHarness"
          />
          <button
            type="button"
            class="agents-mode__default-edit"
            title="Configure harness credentials in Settings, Harness"
            @click="openSandboxSettings"
          >
            Configure…
          </button>
          <span class="agents-mode__default-hint">
            Agents with Harness (use workspace default) fall back to this.
          </span>
        </div>
      </header>

      <div class="agents-mode__split">
        <aside class="agents-mode__rail">
          <div class="agents-mode__search">
            <input
              v-model="search"
              type="search"
              placeholder="Search agents..."
              aria-label="Search agents"
              autocomplete="off"
            />
          </div>
          <div class="agents-mode__rail-list">
            <p v-if="loading" class="agents-mode__empty">Loading agents...</p>
            <template v-else>
              <p v-if="filtered.length === 0 && !draftModel" class="agents-mode__empty">
                {{ search ? 'No matches.' : 'No agents registered.' }}
              </p>

              <button
                v-if="draftModel"
                type="button"
                class="agents-rail__item agents-rail__item--draft agents-rail__item--active"
              >
                <span class="agents-rail__name">{{ draftModel.title || draftModel.slug || '(untitled)' }}</span>
                <span class="agents-rail__meta">draft</span>
              </button>

              <template v-if="builtins.length">
                <div class="agents-rail__group">Built-in</div>
                <button
                  v-for="a in builtins"
                  :key="a.slug"
                  type="button"
                  class="agents-rail__item"
                  :class="{ 'agents-rail__item--active': !draftModel && selectedSlug === a.slug }"
                  @click="selectAgent(a)"
                >
                  <span class="agents-rail__name">{{ a.title || a.slug }}</span>
                  <span v-if="a.harness" class="agents-rail__meta">{{ a.harness }}</span>
                </button>
              </template>

              <template v-if="userAgents.length">
                <div class="agents-rail__group">User-authored</div>
                <button
                  v-for="a in userAgents"
                  :key="a.slug"
                  type="button"
                  class="agents-rail__item agents-rail__item--user"
                  :class="{ 'agents-rail__item--active': !draftModel && selectedSlug === a.slug }"
                  @click="selectAgent(a)"
                >
                  <span class="agents-rail__name">{{ a.title || a.slug }}</span>
                  <span v-if="a.harness" class="agents-rail__meta">{{ a.harness }}</span>
                </button>
              </template>
            </template>
          </div>
        </aside>

        <section class="agents-mode__detail">
          <!-- Empty state -->
          <div v-if="!hasEditor" class="agents-mode__empty-detail">
            <p>Pick an agent on the left, or click <strong>+ New Agent</strong> above.</p>
          </div>

          <!-- Agent editor (create / clone / edit / built-in read-only) -->
          <AgentEditor
            v-else
            v-model:draft="draftModel"
            :agent="editorAgent"
            :is-new="isNew"
            @saved="onSaved"
            @deleted="onDeleted"
            @cancel="onCancel"
            @clone="startClone"
          />
        </section>
      </div>
    </div>
  </div>
</template>
