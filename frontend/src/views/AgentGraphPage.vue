<script setup lang="ts">
import { ref, computed, onMounted, watch } from 'vue';
import { api } from '../api/client';
import type { Agent, Flow, Task, TaskTrace } from '../api/types';
import AgentGraphCanvas from '../components/AgentGraphCanvas.vue';
import AgentEditor from '../components/AgentEditor.vue';
import {
  buildDraftFromFlow,
  newFleetDraft,
  appendStep,
  removeStep,
  promoteToLead,
  coordinationOf,
  setCoordination,
  draftToFlow,
  draftToPayload,
  type Coordination,
  type EditableFlow,
} from '../lib/flowDraft';

// AgentGraphPage is the unified agent-graph surface (spec:
// unified-agent-graph-ui.md). A flow is presented as an agent FLEET: the palette
// (left) is the agent registry; the canvas (center) renders the selected fleet
// (lead + members, delegation edges). Editing clones a built-in or edits a user
// fleet into a draft -- drag agents from the palette to add members, set the
// lead, pick the coordination, remove -- and saves through the flow CRUD. When
// `draft` is null the page is read-only.

const agents = ref<Agent[]>([]);
const flows = ref<Flow[]>([]);
const loading = ref(true);
const search = ref('');
const selectedSlug = ref<string | null>(null);

// Editing state. `draft` is the flow under edit (null = read-only). `saving`
// and `saveError` mirror the FlowsPage editor so the surfaces behave alike.
const draft = ref<EditableFlow | null>(null);
const saving = ref(false);
const saveError = ref('');
const dragOver = ref(false);

const filteredAgents = computed(() => {
  const q = search.value.trim().toLowerCase();
  if (!q) return agents.value.slice();
  return agents.value.filter(
    (a) =>
      (a.slug || '').toLowerCase().includes(q) ||
      (a.title || '').toLowerCase().includes(q) ||
      (a.description || '').toLowerCase().includes(q),
  );
});

const selectedFlow = computed<Flow | null>(() => {
  if (!selectedSlug.value) return null;
  return flows.value.find((f) => f.slug === selectedSlug.value) || null;
});

const flowOptions = computed(() => flows.value);

// The canvas renders the live draft while editing, otherwise the selected flow.
const canvasFlow = computed<Flow | null>(() =>
  draft.value ? draftToFlow(draft.value) : selectedFlow.value,
);

// Run overlay (M6.3): the agentic runs of the selected fleet, and the trace
// status of the chosen run keyed by agent slug for the canvas to color.
const runs = ref<Task[]>([]);
const selectedRunId = ref<string | null>(null);
const runStatus = ref<Record<string, string>>({});

async function loadRuns(slug: string) {
  runs.value = [];
  selectedRunId.value = null;
  runStatus.value = {};
  if (!slug) return;
  try {
    const all = await api<Task[]>('GET', '/api/tasks');
    runs.value = (Array.isArray(all) ? all : [])
      .filter((t) => t.flow_id === slug && !!t.trace)
      .sort((a, b) => (b.created_at || '').localeCompare(a.created_at || ''));
  } catch (e) {
    console.error('runs:', e);
  }
}

async function onSelectRun(id: string | null) {
  selectedRunId.value = id;
  runStatus.value = {};
  if (!id) return;
  try {
    const lin = await api<TaskTrace>('GET', `/api/tasks/${encodeURIComponent(id)}/trace`);
    const status: Record<string, string> = {};
    for (const n of lin.nodes ?? []) {
      // A trace node's name is the agent slug it ran as (agentgraph adapter).
      if (n.name) status[n.name] = n.status;
    }
    runStatus.value = status;
  } catch (e) {
    console.error('trace:', e);
  }
}

// Reload the fleet's runs whenever the selection changes (and not editing).
watch([selectedSlug, draft], ([slug, d]) => {
  confirmingDelete.value = false;
  if (d) return; // editing: no overlay
  void loadRuns(slug ?? '');
});

// startEdit opens the selected flow for editing. A built-in is read-only, so it
// is cloned into a new user flow (saving POSTs); a user flow is edited in place
// (saving PUTs). The full step list is fetched first so a list-view summary
// (which may omit steps) doesn't seed an empty draft.
// startNewFleet opens a blank draft so a fleet can be composed from scratch,
// rather than only by cloning a built-in. Saving POSTs a new user flow.
function startNewFleet() {
  saveError.value = '';
  draft.value = newFleetDraft();
}

async function startEdit() {
  const f = selectedFlow.value;
  if (!f) return;
  saveError.value = '';
  let full = f;
  try {
    full = await api<Flow>('GET', `/api/flows/${encodeURIComponent(f.slug)}`);
  } catch (e) {
    console.warn('flow detail:', e);
  }
  draft.value = buildDraftFromFlow(full, { clone: !!full.builtin });
}

function cancelEdit() {
  draft.value = null;
  saveError.value = '';
}

// onDropAgent adds the dragged palette agent as a new step. Duplicate agents are
// rejected by appendStep (the backend forbids two steps on one agent), surfaced
// as a transient hint rather than a hard error.
function onDropAgent(e: DragEvent) {
  dragOver.value = false;
  if (!draft.value) return;
  const slug = e.dataTransfer?.getData('text/agent-slug') || '';
  if (!slug) return;
  const agent = agents.value.find((a) => a.slug === slug);
  const added = appendStep(draft.value, slug, agent?.title || '');
  if (!added) saveError.value = `"${agent?.title || slug}" is already a step in this flow.`;
  else saveError.value = '';
}

function onAgentDragStart(e: DragEvent, a: Agent) {
  if (!draft.value) return;
  e.dataTransfer?.setData('text/agent-slug', a.slug);
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'copy';
}

function onRemoveStep(agentSlug: string) {
  if (!draft.value) return;
  removeStep(draft.value, agentSlug);
  saveError.value = '';
}

function onSetLead(agentSlug: string) {
  if (!draft.value) return;
  promoteToLead(draft.value, agentSlug);
  saveError.value = '';
}

// coordination is the fleet's lead/mesh/sequence mode, read from and written to
// the draft's agentic/dynamic/topology fields.
const coordination = computed<Coordination>({
  get: () => coordinationOf(draft.value ?? {}),
  set: (c) => {
    if (draft.value) setCoordination(draft.value, c);
  },
});

// Delete a user fleet. Built-ins are read-only; an inline two-step confirm
// keeps the page store-free while still guarding a destructive action.
const confirmingDelete = ref(false);
const deleting = ref(false);
async function deleteFleet() {
  const f = selectedFlow.value;
  if (!f || f.builtin) return;
  deleting.value = true;
  try {
    await api('DELETE', `/api/flows/${encodeURIComponent(f.slug)}`);
    confirmingDelete.value = false;
    selectedSlug.value = null;
    await loadFlows();
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e);
  } finally {
    deleting.value = false;
  }
}

// Agent editor, embedded here so defining an agent and wiring it into a graph
// are one surface (no jump to a separate Agents page). The modal opens over the
// canvas; on save/delete the palette reloads.
const agentEditorOpen = ref(false);
const agentEditorIsNew = ref(false);
const agentEditorAgent = ref<Agent | null>(null);

function openNewAgent() {
  agentEditorAgent.value = null;
  agentEditorIsNew.value = true;
  agentEditorOpen.value = true;
}
// editAgent opens the in-place editor for an agent slug (palette double-click or
// a canvas node). Works whether or not a graph draft is open -- it is a modal,
// so no graph edit is lost.
function editAgent(slug: string) {
  if (!slug) return;
  const a = agents.value.find((x) => x.slug === slug) || null;
  agentEditorAgent.value = a;
  agentEditorIsNew.value = false;
  agentEditorOpen.value = true;
}
function closeAgentEditor() {
  agentEditorOpen.value = false;
  agentEditorIsNew.value = false;
  agentEditorAgent.value = null;
}
async function onAgentSaved() {
  closeAgentEditor();
  await loadAgents();
}
async function onAgentDeleted() {
  closeAgentEditor();
  await loadAgents();
}
function onAgentClone(a: Agent) {
  agentEditorAgent.value = a;
  agentEditorIsNew.value = true;
}

async function saveDraft() {
  if (!draft.value) return;
  if (draft.value.steps.length === 0) {
    saveError.value = 'Add at least one agent before saving.';
    return;
  }
  saving.value = true;
  saveError.value = '';
  try {
    const payload = draftToPayload(draft.value);
    const method = draft.value.isClone ? 'POST' : 'PUT';
    const url = draft.value.isClone
      ? '/api/flows'
      : `/api/flows/${encodeURIComponent(draft.value.slug)}`;
    const saved = await api<Flow>(method, url, payload);
    const slug = saved.slug || payload.slug;
    draft.value = null;
    await loadFlows();
    selectedSlug.value = slug;
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

async function loadAgents() {
  try {
    const rows = await api<Agent[]>('GET', '/api/agents');
    agents.value = Array.isArray(rows) ? rows : [];
  } catch (e) {
    console.error('agents:', e);
  }
}

async function loadFlows() {
  try {
    const rows = await api<Flow[]>('GET', '/api/flows');
    flows.value = Array.isArray(rows) ? rows : [];
    if (!selectedSlug.value && flows.value.length) {
      selectedSlug.value = flows.value[0].slug;
    }
  } catch (e) {
    console.error('flows:', e);
  }
}

onMounted(async () => {
  loading.value = true;
  try {
    await Promise.all([loadAgents(), loadFlows()]);
  } finally {
    loading.value = false;
  }
});
</script>

<template>
  <div class="ag-mode-container">
    <div class="ag-mode__inner">
      <header class="ag-mode__header">
        <div class="ag-mode__header-row">
          <div>
            <h2 class="ag-mode__title">Agent fleets</h2>
            <p class="ag-mode__subtitle">
              A fleet assembles agents to work a task to an outcome. The palette
              lists the agent registry; the canvas shows the selected fleet, with
              the lead agent receiving the task. A fleet either runs its agents in
              a fixed order or lets the lead delegate to members (its coordination).
              <template v-if="draft">Drag agents in, set the lead, pick how they coordinate, then save.</template>
              <template v-else>Start a New fleet, or clone/edit one to compose it; double-click an agent or node to edit the agent.</template>
            </p>
          </div>
          <div class="ag-mode__header-actions">
            <button
              type="button"
              class="btn sm ag-mode__new-fleet"
              :disabled="!!draft"
              title="Start a new empty fleet"
              @click="startNewFleet"
            >+ New fleet</button>
            <label class="ag-mode__flow-pick">
              <span class="eyebrow ag-mode__flow-pick-label">Fleet</span>
              <select v-model="selectedSlug" class="field ag-mode__flow-select" aria-label="Fleet">
                <option v-if="!flowOptions.length" :value="null">No fleets</option>
                <option v-for="f in flowOptions" :key="f.slug" :value="f.slug">
                  {{ f.name || f.slug }}
                </option>
              </select>
            </label>
          </div>
        </div>
      </header>

      <div class="ag-mode__split">
        <aside class="ag-mode__rail">
          <div class="ag-mode__rail-head">
            <input
              v-model="search"
              type="search"
              class="field"
              placeholder="Search agents..."
              aria-label="Search agents"
              autocomplete="off"
            />
            <button type="button" class="btn sm ghost ag-mode__new-agent" @click="openNewAgent" title="Create an agent">
              + New agent
            </button>
          </div>
          <div class="ag-mode__rail-list">
            <p v-if="loading" class="ag-mode__empty">Loading agents...</p>
            <template v-else>
              <p v-if="filteredAgents.length === 0" class="ag-mode__empty">
                {{ search ? 'No matches.' : 'No agents registered.' }}
              </p>
              <div
                v-for="a in filteredAgents"
                :key="a.slug"
                class="ag-card"
                :class="{ 'ag-card--draggable': !!draft, 'ag-card--linkable': !draft }"
                :draggable="!!draft"
                :title="!draft ? 'Double-click to edit this agent' : ''"
                @dragstart="onAgentDragStart($event, a)"
                @dblclick="editAgent(a.slug)"
              >
                <div class="ag-card__head">
                  <span class="ag-card__name">{{ a.title || a.slug }}</span>
                  <span v-if="a.harness" class="pill pill-neutral ag-card__role">{{ a.harness }}</span>
                  <span v-else-if="a.builtin" class="pill pill-neutral ag-card__role">built-in</span>
                </div>
                <p v-if="a.description" class="ag-card__desc">{{ a.description }}</p>
                <code class="ag-card__slug">{{ a.slug }}</code>
              </div>
            </template>
          </div>
        </aside>

        <section class="ag-mode__detail">
          <div v-if="loading" class="ag-mode__empty-detail">
            <p>Loading fleet...</p>
          </div>
          <div v-else-if="!selectedFlow && !draft" class="ag-mode__empty-detail">
            <p>Pick a fleet above to see its agents, or start a New fleet.</p>
          </div>
          <template v-else>
            <div v-if="!draft && selectedFlow" class="ag-detail__head">
              <h3 class="ag-detail__title">{{ selectedFlow.name || selectedFlow.slug }}</h3>
              <span
                class="pill ag-detail__badge"
                :class="selectedFlow.builtin ? 'pill-neutral' : 'pill-brand'"
              >{{ selectedFlow.builtin ? 'built-in' : 'user' }}</span>
              <code class="ag-detail__slug">{{ selectedFlow.slug }}</code>
              <label v-if="runs.length" class="ag-detail__run">
                <span>Run</span>
                <select
                  :value="selectedRunId"
                  class="field ag-detail__run-select"
                  aria-label="Run overlay"
                  @change="onSelectRun(($event.target as HTMLSelectElement).value || null)"
                >
                  <option :value="''">none</option>
                  <option v-for="r in runs" :key="r.id" :value="r.id">
                    {{ r.title || r.id.slice(0, 8) }} ({{ r.status }})
                  </option>
                </select>
              </label>
              <button type="button" class="btn sm ghost ag-detail__edit" @click="startEdit">
                {{ selectedFlow.builtin ? 'Clone & edit' : 'Edit' }}
              </button>
              <template v-if="!selectedFlow.builtin">
                <button
                  v-if="!confirmingDelete"
                  type="button"
                  class="btn sm ghost danger ag-detail__edit ag-detail__delete"
                  @click="confirmingDelete = true"
                >Delete</button>
                <template v-else>
                  <span class="ag-detail__confirm-label">Delete this fleet?</span>
                  <button type="button" class="btn sm ghost danger ag-detail__edit ag-detail__delete" :disabled="deleting" @click="deleteFleet">
                    {{ deleting ? 'Deleting...' : 'Confirm' }}
                  </button>
                  <button type="button" class="btn sm ghost ag-detail__edit" :disabled="deleting" @click="confirmingDelete = false">Keep</button>
                </template>
              </template>
            </div>

            <!-- Editing toolbar: name + slug (slug locked when editing in place,
                 editable when naming a clone), the topology controls, and the
                 save/cancel actions. -->
            <div v-else-if="draft" class="ag-edit">
              <div class="ag-edit__fields">
                <input
                  v-model="draft.name"
                  class="field ag-edit__name"
                  placeholder="Fleet name"
                  aria-label="Fleet name"
                />
                <input
                  v-model="draft.slug"
                  class="field mono ag-edit__slug"
                  :readonly="!draft.isClone"
                  placeholder="fleet-slug"
                  aria-label="Fleet slug"
                />
                <span v-if="draft.isClone" class="ag-edit__hint">clone of {{ draft.sourceSlug }}</span>
              </div>

              <!-- Coordination: how the graph works a task. Fixed sequence is
                   the production path (runs the agents through the engine with
                   real worktrees/commits). The delegating modes (Lead delegates,
                   Open mesh) run through the topos runtime, which does not yet
                   produce durable commits (spike S NO-GO) -- so they are marked
                   experimental until the worktree-sandbox adapter lands. -->
              <div class="ag-edit__topo">
                <label class="eyebrow ag-edit__field-label">Coordination</label>
                <select v-model="coordination" class="field ag-edit__topo-select" aria-label="Coordination">
                  <option value="sequence">Fixed sequence</option>
                  <option value="lead">Lead delegates (experimental)</option>
                  <option value="mesh">Open mesh (experimental)</option>
                </select>
                <label v-if="coordination === 'mesh'" class="ag-edit__depth">
                  <span>handoff depth</span>
                  <input
                    type="number"
                    min="0"
                    v-model.number="draft.max_handoff_depth"
                    class="field ag-edit__depth-input"
                    aria-label="Max handoff depth"
                  />
                </label>
              </div>
              <p v-if="coordination !== 'sequence'" class="ag-edit__experimental">
                Experimental: delegating graphs run through the topos runtime,
                which does not yet make durable commits or run verification. Use
                Fixed sequence for real task runs.
              </p>

              <div class="ag-edit__actions">
                <button type="button" class="btn sm ghost ag-edit__btn" @click="cancelEdit" :disabled="saving">Cancel</button>
                <button type="button" class="btn sm ag-edit__btn ag-edit__btn--save" @click="saveDraft" :disabled="saving">
                  {{ saving ? 'Saving...' : 'Save' }}
                </button>
              </div>
            </div>

            <p v-if="draft && saveError" class="ag-edit__error">{{ saveError }}</p>
            <p v-else-if="!draft && selectedFlow?.description" class="ag-detail__desc">
              {{ selectedFlow?.description }}
            </p>
            <p v-else-if="draft" class="ag-edit__tip">
              Drag an agent from the palette to add a member; hover a member for
              its controls (★ make lead, × remove). The lead receives the task.
            </p>

            <div
              class="ag-detail__canvas"
              :class="{ 'ag-detail__canvas--drop': dragOver, 'ag-detail__canvas--editing': !!draft }"
              @dragover.prevent="draft && (dragOver = true)"
              @dragleave="dragOver = false"
              @drop.prevent="onDropAgent"
            >
              <AgentGraphCanvas
                :flow="canvasFlow"
                :editable="!!draft"
                :run-status="draft ? undefined : runStatus"
                @remove="onRemoveStep"
                @set-lead="onSetLead"
                @edit-agent="editAgent"
              />
            </div>
          </template>
        </section>
      </div>
    </div>

    <!-- Agent editor (create / clone / edit / delete), embedded so agent
         definition lives on the same surface as graph composition. -->
    <div v-if="agentEditorOpen" class="ag-agent-modal" @click.self="closeAgentEditor">
      <div class="ag-agent-modal__panel">
        <button type="button" class="icon-btn ag-agent-modal__close" aria-label="Close agent editor" @click="closeAgentEditor"><svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><line x1="18" y1="6" x2="6" y2="18"></line><line x1="6" y1="6" x2="18" y2="18"></line></svg></button>
        <AgentEditor
          :agent="agentEditorAgent"
          :is-new="agentEditorIsNew"
          @saved="onAgentSaved"
          @deleted="onAgentDeleted"
          @cancel="closeAgentEditor"
          @clone="onAgentClone"
        />
      </div>
    </div>
  </div>
</template>

<style scoped>
/* Agent fleets: a 280px sunk list of agent rows, a canvas card, and the
   editor as a dialog. See specs/shared/console-redesign/agent-graph.md. */
.ag-mode-container {
  height: 100%;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}
.ag-mode__inner {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
  padding: 20px 24px;
  gap: 16px;
}
.ag-mode__header-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}
.ag-mode__title {
  margin: 0;
  font-size: var(--fs-2xl);
  font-weight: 600;
  letter-spacing: -0.015em;
  line-height: 1.2;
}
.ag-mode__subtitle {
  margin: 6px 0 0;
  max-width: 68ch;
  font-size: var(--fs-base);
  color: var(--ink-2);
  line-height: 1.5;
}
.ag-mode__header-actions {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  flex: none;
}
.ag-mode__flow-pick {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}
.ag-mode__flow-select {
  width: auto;
  min-width: 180px;
}
.ag-mode__split {
  flex: 1;
  min-height: 0;
  display: grid;
  grid-template-columns: 280px 1fr;
  gap: 16px;
}
/* The agent list */
.ag-mode__rail {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border: 1px solid var(--rule);
  border-radius: var(--r-xl);
  background: var(--bg-sunk);
  overflow: hidden;
}
.ag-mode__rail-head {
  display: flex;
  gap: 6px;
  padding: 10px;
  border-bottom: 1px solid var(--rule);
}
.ag-mode__rail-head .field {
  flex: 1;
  min-width: 0;
}
.ag-mode__new-agent {
  flex: none;
}
.ag-mode__rail-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 6px;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.ag-mode__empty {
  margin: 8px 6px;
  font-size: 12px;
  color: var(--ink-3);
}
.ag-card {
  padding: 8px 10px;
  border-radius: var(--r-row);
  border: 1px solid transparent;
  transition: background var(--dur-hover), border-color var(--dur-hover);
}
.ag-card--linkable {
  cursor: pointer;
}
.ag-card--linkable:hover {
  background: color-mix(in srgb, var(--ink) 5%, transparent);
}
.ag-card--draggable {
  cursor: grab;
  background: var(--bg-card);
  border-color: var(--rule);
  box-shadow: var(--sh-card);
}
.ag-card--draggable:active {
  cursor: grabbing;
}
.ag-card__head {
  display: flex;
  align-items: center;
  gap: 8px;
}
.ag-card__name {
  flex: 1;
  min-width: 0;
  font-size: var(--fs-base);
  font-weight: 600;
  color: var(--ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ag-card__role {
  flex: none;
}
.ag-card__desc {
  margin: 3px 0 0;
  font-size: 12px;
  color: var(--ink-2);
  line-height: 1.4;
}
.ag-card__slug {
  display: inline-block;
  margin-top: 4px;
  font: 400 var(--fs-9) / 1 var(--font-mono);
  color: var(--ink-4);
}
/* The fleet detail */
.ag-mode__detail {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border: 1px solid var(--rule);
  border-radius: var(--r-xl);
  background: var(--bg-card);
  padding: 14px 16px;
  overflow: hidden;
  box-shadow: var(--sh-card);
}
.ag-mode__empty-detail {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--ink-3);
  font-size: var(--fs-base);
}
.ag-detail__head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.ag-detail__title {
  margin: 0;
  font-size: var(--fs-xl);
  font-weight: 600;
}
.ag-detail__slug {
  font: 400 var(--fs-10) / 1 var(--font-mono);
  color: var(--ink-4);
}
.ag-detail__run {
  margin-left: auto;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--ink-3);
}
.ag-detail__run-select {
  width: auto;
  min-height: 28px;
  padding: 2px 8px;
  font-size: 12px;
}
.ag-detail__run + .ag-detail__edit {
  margin-left: 0;
}
.ag-detail__edit {
  margin-left: auto;
}
.ag-detail__edit ~ .ag-detail__edit {
  margin-left: 0;
}
.ag-detail__confirm-label {
  font-size: 12px;
  color: var(--err);
}
.ag-detail__desc {
  margin: 8px 0 0;
  font-size: var(--fs-base);
  color: var(--ink-2);
}
/* Editing toolbar */
.ag-edit {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.ag-edit__fields {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.ag-edit__name {
  width: 220px;
  font-weight: 600;
}
.ag-edit__slug {
  width: 180px;
}
.ag-edit__slug[readonly] {
  color: var(--ink-3);
}
.ag-edit__hint {
  font: 400 var(--fs-9) / 1 var(--font-mono);
  color: var(--ink-4);
}
.ag-edit__topo {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  font-size: 12px;
  color: var(--ink-2);
}
.ag-edit__topo-select {
  width: auto;
}
.ag-edit__experimental {
  flex-basis: 100%;
  margin: 4px 0 0;
  padding: 6px 10px;
  border-radius: var(--r-sm);
  background: var(--tint-amber);
  font-size: 12px;
  color: var(--warn);
}
.ag-edit__depth {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.ag-edit__depth-input {
  width: 64px;
  text-align: center;
}
.ag-edit__actions {
  display: flex;
  gap: 6px;
}
.ag-edit__error {
  margin: 8px 0 0;
  padding: 6px 10px;
  border-radius: var(--r-sm);
  background: var(--tint-red);
  font-size: 12px;
  color: var(--err);
}
.ag-edit__tip {
  margin: 8px 0 0;
  font-size: 12px;
  color: var(--ink-3);
}
/* The canvas well */
.ag-detail__canvas {
  flex: 1;
  min-height: 0;
  margin-top: 12px;
  border: 1px solid var(--rule);
  border-radius: var(--r-lg);
  background: var(--bg-sunk);
  overflow: auto;
  transition: border-color var(--dur-hover), background var(--dur-hover);
}
.ag-detail__canvas--editing {
  border-style: dashed;
  border-color: var(--accent-line);
}
.ag-detail__canvas--drop {
  border-color: var(--accent);
  background: var(--accent-soft);
}
/* The agent editor dialog */
.ag-agent-modal {
  position: fixed;
  inset: 0;
  z-index: 50;
  display: flex;
  align-items: flex-start;
  justify-content: center;
  padding: 48px 16px;
  background: var(--glass-dim);
  overflow: auto;
  animation: om-fade-in 0.16s var(--ease-fluid) both;
}
.ag-agent-modal__panel {
  position: relative;
  width: min(720px, 100%);
  background: var(--bg);
  border: 1px solid var(--rule);
  border-radius: var(--r-main);
  padding: 20px 22px;
  box-shadow: var(--sh-pop);
  animation: om-fade-up 0.2s var(--ease-fluid) both;
}
.ag-agent-modal__close {
  position: absolute;
  top: 12px;
  right: 12px;
}
</style>
