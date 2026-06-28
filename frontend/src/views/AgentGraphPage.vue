<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { api } from '../api/client';
import type { Agent, Flow } from '../api/types';
import AgentGraphCanvas from '../components/AgentGraphCanvas.vue';
import {
  buildDraftFromFlow,
  appendStep,
  removeStep,
  setParallel,
  clearParallel,
  moveStage,
  draftToFlow,
  draftToPayload,
  type EditableFlow,
} from '../lib/flowDraft';

// AgentGraphPage is the unified agent-graph surface (spec:
// unified-agent-graph-ui.md). Left is the agent palette (the merged registry,
// searchable); centre renders a selected flow as an agent-graph. M6.1 was the
// read-only scaffold; M6.2 adds editing: a flow is cloned (built-ins are
// read-only) or edited in place into a draft, agents are dragged from the
// palette onto the canvas to add steps, and the draft is saved through the flow
// CRUD. When `draft` is null the page is read-only, which keeps the original
// render path (and its component test) intact.

const router = useRouter();

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

// startEdit opens the selected flow for editing. A built-in is read-only, so it
// is cloned into a new user flow (saving POSTs); a user flow is edited in place
// (saving PUTs). The full step list is fetched first so a list-view summary
// (which may omit steps) doesn't seed an empty draft.
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

function onParallel(p: { from: string; to: string }) {
  if (!draft.value) return;
  setParallel(draft.value, p.from, p.to);
  saveError.value = '';
}

function onUngroup(agentSlug: string) {
  if (!draft.value) return;
  clearParallel(draft.value, agentSlug);
  saveError.value = '';
}

function onReorder(p: { slug: string; toStage: number }) {
  if (!draft.value) return;
  moveStage(draft.value, p.slug, p.toStage);
  saveError.value = '';
}

// editAgent jumps to the existing Agents editor for an agent (deep link read by
// AgentsPage). Suppressed while editing a flow so an unsaved draft is not lost
// to the navigation; the affordance is for the read-only graph.
function editAgent(slug: string) {
  if (draft.value || !slug) return;
  void router.push({ path: '/agents', query: { agent: slug } });
}

async function saveDraft() {
  if (!draft.value) return;
  if (draft.value.steps.length === 0) {
    saveError.value = 'Add at least one step before saving.';
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
            <h2 class="ag-mode__title">Agent Graph</h2>
            <p class="ag-mode__subtitle">
              One surface for agents and flows. The palette on the left lists the
              agent registry; the canvas renders a flow as an agent-graph, with a
              node per step and edges for order.
              <template v-if="draft">Drag agents onto the canvas to add steps, then save.</template>
              <template v-else>Clone or edit a flow to compose it; double-click an agent or node to edit the agent.</template>
            </p>
          </div>
          <div class="ag-mode__header-actions">
            <label class="ag-mode__flow-pick">
              <span class="ag-mode__flow-pick-label">Flow</span>
              <select v-model="selectedSlug" class="ag-mode__flow-select" aria-label="Flow">
                <option v-if="!flowOptions.length" :value="null">No flows</option>
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
          <div class="ag-mode__search">
            <input
              v-model="search"
              type="search"
              placeholder="Search agents..."
              aria-label="Search agents"
              autocomplete="off"
            />
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
                  <span v-if="a.harness" class="ag-card__role">{{ a.harness }}</span>
                  <span v-else-if="a.builtin" class="ag-card__role">built-in</span>
                </div>
                <p v-if="a.description" class="ag-card__desc">{{ a.description }}</p>
                <code class="ag-card__slug">{{ a.slug }}</code>
              </div>
            </template>
          </div>
        </aside>

        <section class="ag-mode__detail">
          <div v-if="loading" class="ag-mode__empty-detail">
            <p>Loading flow...</p>
          </div>
          <div v-else-if="!selectedFlow" class="ag-mode__empty-detail">
            <p>Pick a flow above to render its agent-graph.</p>
          </div>
          <template v-else>
            <div v-if="!draft" class="ag-detail__head">
              <h3 class="ag-detail__title">{{ selectedFlow.name || selectedFlow.slug }}</h3>
              <span
                class="ag-detail__badge"
                :class="{ 'ag-detail__badge--user': !selectedFlow.builtin }"
              >{{ selectedFlow.builtin ? 'built-in' : 'user' }}</span>
              <code class="ag-detail__slug">{{ selectedFlow.slug }}</code>
              <button type="button" class="ag-detail__edit" @click="startEdit">
                {{ selectedFlow.builtin ? 'Clone & edit' : 'Edit' }}
              </button>
            </div>

            <!-- Editing toolbar: name + slug (slug locked when editing in place,
                 editable when naming a clone), the topology controls, and the
                 save/cancel actions. -->
            <div v-else class="ag-edit">
              <div class="ag-edit__fields">
                <input
                  v-model="draft.name"
                  class="ag-edit__name"
                  placeholder="Flow name"
                  aria-label="Flow name"
                />
                <input
                  v-model="draft.slug"
                  class="ag-edit__slug"
                  :readonly="!draft.isClone"
                  placeholder="flow-slug"
                  aria-label="Flow slug"
                />
                <span v-if="draft.isClone" class="ag-edit__hint">clone of {{ draft.sourceSlug }}</span>
              </div>

              <!-- Topology: a pinned chain runs the steps deterministically; an
                   agentic flow runs through the topos runtime, and a dynamic one
                   lets the model delegate (orchestrator-worker or mesh, bounded
                   by the handoff depth). Maps onto the M6.2a flow fields. -->
              <div class="ag-edit__topo">
                <label class="ag-edit__toggle">
                  <input type="checkbox" v-model="draft.agentic" aria-label="Agentic" />
                  <span>Agentic</span>
                </label>
                <label v-if="draft.agentic" class="ag-edit__toggle">
                  <input type="checkbox" v-model="draft.dynamic" aria-label="Dynamic" />
                  <span>Dynamic</span>
                </label>
                <select
                  v-if="draft.agentic && draft.dynamic"
                  v-model="draft.topology"
                  class="ag-edit__topo-select"
                  aria-label="Topology"
                >
                  <option value="orchestrator-worker">orchestrator-worker</option>
                  <option value="mesh">mesh</option>
                </select>
                <label v-if="draft.agentic && draft.dynamic" class="ag-edit__depth">
                  <span>depth</span>
                  <input
                    type="number"
                    min="0"
                    v-model.number="draft.max_handoff_depth"
                    class="ag-edit__depth-input"
                    aria-label="Max handoff depth"
                  />
                </label>
              </div>

              <div class="ag-edit__actions">
                <button type="button" class="ag-edit__btn" @click="cancelEdit" :disabled="saving">Cancel</button>
                <button type="button" class="ag-edit__btn ag-edit__btn--save" @click="saveDraft" :disabled="saving">
                  {{ saving ? 'Saving...' : 'Save' }}
                </button>
              </div>
            </div>

            <p v-if="draft && saveError" class="ag-edit__error">{{ saveError }}</p>
            <p v-else-if="!draft && selectedFlow.description" class="ag-detail__desc">
              {{ selectedFlow.description }}
            </p>
            <p v-else-if="draft" class="ag-edit__tip">
              Drag an agent from the palette to add a step; drag one step onto
              another to run them in parallel, or into a gap to reorder.
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
                @remove="onRemoveStep"
                @parallel="onParallel"
                @ungroup="onUngroup"
                @reorder="onReorder"
                @edit-agent="editAgent"
              />
            </div>
          </template>
        </section>
      </div>
    </div>
  </div>
</template>

<style scoped>
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
  padding: 1.1rem 1.25rem;
  gap: 1rem;
}
.ag-mode__header-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
}
.ag-mode__title {
  margin: 0;
  font-size: 1.25rem;
}
.ag-mode__subtitle {
  margin: 0.3rem 0 0;
  max-width: 46rem;
  font-size: 0.84rem;
  color: var(--text-secondary);
}
.ag-mode__flow-pick {
  display: inline-flex;
  align-items: center;
  gap: 0.45rem;
  font-size: 0.78rem;
  color: var(--text-secondary);
}
.ag-mode__flow-select {
  font: inherit;
  padding: 0.35rem 0.5rem;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-elevated);
  color: var(--text);
}
.ag-mode__split {
  flex: 1;
  min-height: 0;
  display: grid;
  grid-template-columns: 280px 1fr;
  gap: 1rem;
}
.ag-mode__rail {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-elevated);
  overflow: hidden;
}
.ag-mode__search {
  padding: 0.6rem;
  border-bottom: 1px solid var(--border);
}
.ag-mode__search input {
  width: 100%;
  font: inherit;
  padding: 0.4rem 0.55rem;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-sunk);
  color: var(--text);
}
.ag-mode__rail-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 0.6rem;
  display: flex;
  flex-direction: column;
  gap: 0.5rem;
}
.ag-mode__empty {
  margin: 0.5rem 0;
  font-size: 0.8rem;
  color: var(--text-secondary);
}
.ag-card {
  border: 1px solid var(--border);
  border-radius: 9px;
  background: var(--bg-sunk);
  padding: 0.55rem 0.65rem;
}
.ag-card--linkable {
  cursor: pointer;
}
.ag-card--draggable {
  cursor: grab;
  border-color: color-mix(in srgb, var(--accent) 35%, var(--border));
}
.ag-card--draggable:active {
  cursor: grabbing;
}
.ag-card__head {
  display: flex;
  align-items: baseline;
  gap: 0.45rem;
}
.ag-card__name {
  font-size: 0.84rem;
  font-weight: 600;
  color: var(--text);
}
.ag-card__role {
  font-size: 0.66rem;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  color: var(--text-muted);
}
.ag-card__desc {
  margin: 0.25rem 0 0;
  font-size: 0.76rem;
  color: var(--text-secondary);
}
.ag-card__slug {
  display: inline-block;
  margin-top: 0.3rem;
  font-size: 0.7rem;
  color: var(--text-muted);
}
.ag-mode__detail {
  display: flex;
  flex-direction: column;
  min-height: 0;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-elevated);
  padding: 0.9rem 1rem;
  overflow: hidden;
}
.ag-mode__empty-detail {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--text-secondary);
  font-size: 0.85rem;
}
.ag-detail__head {
  display: flex;
  align-items: baseline;
  gap: 0.5rem;
}
.ag-detail__title {
  margin: 0;
  font-size: 1.02rem;
}
.ag-detail__badge {
  font-size: 0.64rem;
  text-transform: uppercase;
  letter-spacing: 0.03em;
  padding: 0.1rem 0.4rem;
  border-radius: 999px;
  color: var(--text-muted);
  background: var(--bg-hover);
}
.ag-detail__badge--user {
  color: var(--accent);
  background: color-mix(in srgb, var(--accent) 14%, transparent);
}
.ag-detail__slug {
  font-size: 0.72rem;
  color: var(--text-muted);
}
.ag-detail__edit {
  margin-left: auto;
  font: inherit;
  font-size: 0.74rem;
  padding: 0.28rem 0.7rem;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-sunk);
  color: var(--text);
  cursor: pointer;
}
.ag-detail__edit:hover {
  border-color: var(--accent);
}
.ag-detail__desc {
  margin: 0.45rem 0 0;
  font-size: 0.8rem;
  color: var(--text-secondary);
}
.ag-edit {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 1rem;
  flex-wrap: wrap;
}
.ag-edit__fields {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  flex-wrap: wrap;
}
.ag-edit__name,
.ag-edit__slug {
  font: inherit;
  padding: 0.32rem 0.5rem;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-sunk);
  color: var(--text);
}
.ag-edit__name {
  font-size: 0.95rem;
  font-weight: 600;
}
.ag-edit__slug {
  font-size: 0.78rem;
  font-family: var(--font-mono, monospace);
  max-width: 13rem;
}
.ag-edit__slug[readonly] {
  color: var(--text-muted);
  opacity: 0.8;
}
.ag-edit__hint {
  font-size: 0.7rem;
  color: var(--text-muted);
}
.ag-edit__topo {
  display: flex;
  align-items: center;
  gap: 0.7rem;
  flex-wrap: wrap;
  font-size: 0.78rem;
  color: var(--text-secondary);
}
.ag-edit__toggle {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
  cursor: pointer;
}
.ag-edit__topo-select {
  font: inherit;
  font-size: 0.76rem;
  padding: 0.25rem 0.4rem;
  border-radius: 7px;
  border: 1px solid var(--border);
  background: var(--bg-sunk);
  color: var(--text);
}
.ag-edit__depth {
  display: inline-flex;
  align-items: center;
  gap: 0.3rem;
}
.ag-edit__depth-input {
  width: 3.2rem;
  font: inherit;
  font-size: 0.76rem;
  padding: 0.25rem 0.35rem;
  border-radius: 7px;
  border: 1px solid var(--border);
  background: var(--bg-sunk);
  color: var(--text);
}
.ag-edit__actions {
  display: flex;
  gap: 0.45rem;
}
.ag-edit__btn {
  font: inherit;
  font-size: 0.78rem;
  padding: 0.32rem 0.85rem;
  border-radius: 8px;
  border: 1px solid var(--border);
  background: var(--bg-sunk);
  color: var(--text);
  cursor: pointer;
}
.ag-edit__btn--save {
  border-color: var(--accent);
  background: color-mix(in srgb, var(--accent) 16%, transparent);
  color: var(--accent);
  font-weight: 600;
}
.ag-edit__btn:disabled {
  opacity: 0.55;
  cursor: default;
}
.ag-edit__error {
  margin: 0.45rem 0 0;
  font-size: 0.78rem;
  color: var(--danger, #d2453f);
}
.ag-edit__tip {
  margin: 0.45rem 0 0;
  font-size: 0.78rem;
  color: var(--text-secondary);
}
.ag-detail__canvas {
  flex: 1;
  min-height: 0;
  margin-top: 0.75rem;
  border: 1px solid var(--border);
  border-radius: 10px;
  background: var(--bg-sunk);
  overflow: auto;
}
.ag-detail__canvas--editing {
  border-style: dashed;
  border-color: color-mix(in srgb, var(--accent) 40%, var(--border));
}
.ag-detail__canvas--drop {
  border-color: var(--accent);
  background: color-mix(in srgb, var(--accent) 8%, var(--bg-sunk));
}
</style>
