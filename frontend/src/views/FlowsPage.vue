<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import draggable from 'vuedraggable';
import { api, ApiError } from '../api/client';

interface FlowStep {
  agent_slug: string;
  agent_name?: string;
  optional?: boolean;
  input_from?: string;
  run_in_parallel_with?: string[];
}
interface Flow {
  slug: string;
  name: string;
  description?: string;
  builtin: boolean;
  steps?: FlowStep[];
  spawn_kind?: string;
}
interface AgentRow { slug: string; title: string }
interface DraftStep {
  agent_slug: string;
  optional: boolean;
  input_from: string;
  run_in_parallel_with: string[];
}
interface Draft {
  slug: string;
  name: string;
  description: string;
  steps: DraftStep[];
}

const flows = ref<Flow[]>([]);
const agentsCache = ref<AgentRow[]>([]);
const loading = ref(true);
const search = ref('');
const selectedSlug = ref<string | null>(null);
const draft = ref<Draft | null>(null);
const editingDraft = ref<Draft | null>(null);
const detailCache = ref<Record<string, Flow>>({});
const detailLoading = ref(false);
const saveError = ref('');
const saving = ref(false);

const filtered = computed(() => {
  const q = search.value.trim().toLowerCase();
  if (!q) return flows.value.slice();
  return flows.value.filter((f) => {
    return (
      (f.slug || '').toLowerCase().includes(q) ||
      (f.name || '').toLowerCase().includes(q) ||
      (f.description || '').toLowerCase().includes(q)
    );
  });
});
const builtins = computed(() => filtered.value.filter((f) => f.builtin));
const userFlows = computed(() => filtered.value.filter((f) => !f.builtin));

const selectedFlow = computed(() => {
  if (draft.value) return null;
  if (!selectedSlug.value) return null;
  return flows.value.find((f) => f.slug === selectedSlug.value) || null;
});
const selectedDetail = computed(() => {
  if (!selectedFlow.value) return null;
  return detailCache.value[selectedFlow.value.slug] || selectedFlow.value;
});

async function loadFlows() {
  loading.value = true;
  try {
    const rows = await api<Flow[]>('GET', '/api/flows');
    flows.value = Array.isArray(rows) ? rows : [];
    if (selectedSlug.value && !draft.value && !flows.value.find((f) => f.slug === selectedSlug.value)) {
      selectedSlug.value = null;
    }
  } catch (e) {
    console.error('flows:', e);
  } finally {
    loading.value = false;
  }
}

async function ensureAgents() {
  if (agentsCache.value.length) return;
  try {
    const rows = await api<AgentRow[]>('GET', '/api/agents');
    agentsCache.value = Array.isArray(rows) ? rows : [];
  } catch (e) {
    console.warn('agents fetch:', e);
  }
}

async function selectFlow(f: Flow) {
  draft.value = null;
  editingDraft.value = null;
  selectedSlug.value = f.slug;
  if (!detailCache.value[f.slug]) {
    detailLoading.value = true;
    try {
      const full = await api<Flow>('GET', `/api/flows/${encodeURIComponent(f.slug)}`);
      detailCache.value[f.slug] = full;
    } catch (e) {
      console.error('flow detail:', e);
    } finally {
      detailLoading.value = false;
    }
  }
}

function openNewEditor() {
  draft.value = {
    slug: 'my-flow',
    name: '',
    description: '',
    steps: [{ agent_slug: '', optional: false, input_from: '', run_in_parallel_with: [] }],
  };
  selectedSlug.value = null;
  saveError.value = '';
  void ensureAgents();
}

async function startClone(flow: Flow) {
  let source = detailCache.value[flow.slug];
  if (!source) {
    try {
      source = await api<Flow>('GET', `/api/flows/${encodeURIComponent(flow.slug)}`);
      detailCache.value[flow.slug] = source;
    } catch (e) { console.warn('clone fetch:', e); source = flow; }
  }
  draft.value = {
    slug: suggestCloneSlug(flow.slug),
    name: (flow.name || '') + ' (copy)',
    description: source.description || '',
    steps: (source.steps || []).map((s) => ({
      agent_slug: s.agent_slug || '',
      optional: !!s.optional,
      input_from: s.input_from || '',
      run_in_parallel_with: (s.run_in_parallel_with || []).slice(),
    })),
  };
  if (draft.value.steps.length === 0) {
    draft.value.steps.push({ agent_slug: '', optional: false, input_from: '', run_in_parallel_with: [] });
  }
  selectedSlug.value = null;
  saveError.value = '';
  void ensureAgents();
}

function suggestCloneSlug(base: string): string {
  const s = base + '-copy';
  return s.length <= 40 ? s : base.slice(0, 35) + '-copy';
}

function startEdit() {
  if (!selectedFlow.value) return;
  const src = selectedDetail.value || selectedFlow.value;
  editingDraft.value = {
    slug: src.slug,
    name: src.name || '',
    description: src.description || '',
    steps: (src.steps || []).map((s) => ({
      agent_slug: s.agent_slug || '',
      optional: !!s.optional,
      input_from: s.input_from || '',
      run_in_parallel_with: (s.run_in_parallel_with || []).slice(),
    })),
  };
  if (editingDraft.value.steps.length === 0) {
    editingDraft.value.steps.push({ agent_slug: '', optional: false, input_from: '', run_in_parallel_with: [] });
  }
  void ensureAgents();
}

function cancelEdit() {
  draft.value = null;
  editingDraft.value = null;
  saveError.value = '';
}

function addStep(target: Draft) {
  target.steps.push({ agent_slug: '', optional: false, input_from: '', run_in_parallel_with: [] });
}
function removeStep(target: Draft, idx: number) {
  if (target.steps.length <= 1) return;
  target.steps.splice(idx, 1);
}

async function saveDraft() {
  if (!draft.value) return;
  saving.value = true;
  saveError.value = '';
  try {
    const payload = {
      slug: draft.value.slug.trim(),
      name: draft.value.name.trim(),
      description: draft.value.description.trim(),
      steps: draft.value.steps.map((s) => ({
        agent_slug: s.agent_slug,
        optional: s.optional,
        input_from: s.input_from,
        run_in_parallel_with: s.run_in_parallel_with,
      })),
    };
    const saved = await api<Flow>('POST', '/api/flows', payload);
    draft.value = null;
    selectedSlug.value = saved.slug || payload.slug;
    delete detailCache.value[selectedSlug.value!];
    await loadFlows();
    const f = flows.value.find((x) => x.slug === selectedSlug.value);
    if (f) await selectFlow(f);
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

async function saveEdit() {
  if (!editingDraft.value || !selectedFlow.value) return;
  saving.value = true;
  saveError.value = '';
  try {
    const payload = {
      slug: editingDraft.value.slug,
      name: editingDraft.value.name.trim(),
      description: editingDraft.value.description.trim(),
      steps: editingDraft.value.steps.map((s) => ({
        agent_slug: s.agent_slug,
        optional: s.optional,
        input_from: s.input_from,
        run_in_parallel_with: s.run_in_parallel_with,
      })),
    };
    await api(
      'PUT',
      `/api/flows/${encodeURIComponent(selectedFlow.value.slug)}`,
      payload,
    );
    const slug = selectedFlow.value.slug;
    delete detailCache.value[slug];
    editingDraft.value = null;
    await loadFlows();
    const f = flows.value.find((x) => x.slug === slug);
    if (f) await selectFlow(f);
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

async function deleteFlow(slug: string) {
  if (!window.confirm(`Delete flow ${slug}?`)) return;
  try {
    await api('DELETE', `/api/flows/${encodeURIComponent(slug)}`);
    selectedSlug.value = null;
    delete detailCache.value[slug];
    await loadFlows();
  } catch (e) {
    if (e instanceof ApiError) {
      window.alert('Delete failed: ' + e.message);
    } else {
      window.alert('Delete failed: ' + String(e));
    }
  }
}

// Group steps into parallel clusters using transitive closure on
// run_in_parallel_with (matches the engine's behaviour).
function groupParallel(steps: FlowStep[]): FlowStep[][] {
  const bySlug: Record<string, number> = {};
  steps.forEach((s, i) => { bySlug[s.agent_slug] = i; });
  const adj: number[][] = steps.map((s) => {
    const peers: number[] = [];
    (s.run_in_parallel_with || []).forEach((p) => {
      if (typeof bySlug[p] === 'number' && bySlug[p] !== bySlug[s.agent_slug]) {
        peers.push(bySlug[p]);
      }
    });
    return peers;
  });
  const assigned = steps.map(() => -1);
  const groups: FlowStep[][] = [];
  steps.forEach((_, i) => {
    if (assigned[i] !== -1) return;
    const gid = groups.length;
    const queue = [i];
    assigned[i] = gid;
    const members = [i];
    while (queue.length) {
      const cur = queue.shift()!;
      adj[cur].forEach((n) => {
        if (assigned[n] === -1) {
          assigned[n] = gid;
          members.push(n);
          queue.push(n);
        }
      });
    }
    members.sort((a, b) => a - b);
    groups.push(members.map((idx) => steps[idx]));
  });
  return groups;
}

function chipLabel(step: FlowStep): string {
  const base = step.agent_name || step.agent_slug;
  return step.optional ? base + '?' : base;
}

const router = useRouter();
function gotoAgents() {
  void router.push('/agents');
}

const chainGroups = computed(() => {
  const steps = selectedDetail.value?.steps || [];
  return groupParallel(steps);
});

onMounted(async () => {
  await loadFlows();
});
</script>

<template>
  <div class="flows-mode-container">
    <div class="flows-mode__inner">
      <header class="flows-mode__header">
        <div class="flows-mode__header-row">
          <div>
            <h2 class="flows-mode__title">Flows</h2>
            <p class="flows-mode__subtitle">
              A flow is an ordered chain of sub-agents a task runs against. Clone
              a built-in or start from scratch; reorder steps by drag, mark any
              step optional, or group steps to run in parallel.
            </p>
          </div>
          <div class="flows-mode__header-actions">
            <button type="button" class="flows-mode__new" @click="openNewEditor">
              + New Flow
            </button>
          </div>
        </div>
      </header>

      <div class="flows-mode__split">
        <aside class="flows-mode__rail">
          <div class="flows-mode__search">
            <input
              v-model="search"
              type="search"
              placeholder="Search flows..."
              aria-label="Search flows"
              autocomplete="off"
            />
          </div>
          <div class="flows-mode__rail-list">
            <p v-if="loading" class="flows-mode__empty">Loading flows...</p>
            <template v-else>
              <p v-if="filtered.length === 0 && !draft" class="flows-mode__empty">
                {{ search ? 'No matches.' : 'No flows registered.' }}
              </p>

              <button
                v-if="draft"
                type="button"
                class="flows-rail__item flows-rail__item--draft flows-rail__item--active"
              >
                <span class="flows-rail__name">{{ draft.name || draft.slug || '(untitled)' }}</span>
                <span class="flows-rail__meta">draft</span>
              </button>

              <template v-if="builtins.length">
                <div class="flows-rail__group">Built-in</div>
                <button
                  v-for="f in builtins"
                  :key="f.slug"
                  type="button"
                  class="flows-rail__item"
                  :class="{ 'flows-rail__item--active': !draft && selectedSlug === f.slug }"
                  @click="selectFlow(f)"
                >
                  <span class="flows-rail__name">{{ f.name || f.slug }}</span>
                  <span class="flows-rail__meta">
                    {{ (f.steps?.length ?? 0) }} step{{ (f.steps?.length ?? 0) === 1 ? '' : 's' }}
                  </span>
                </button>
              </template>

              <template v-if="userFlows.length">
                <div class="flows-rail__group">User-authored</div>
                <button
                  v-for="f in userFlows"
                  :key="f.slug"
                  type="button"
                  class="flows-rail__item flows-rail__item--user"
                  :class="{ 'flows-rail__item--active': !draft && selectedSlug === f.slug }"
                  @click="selectFlow(f)"
                >
                  <span class="flows-rail__name">{{ f.name || f.slug }}</span>
                  <span class="flows-rail__meta">
                    {{ (f.steps?.length ?? 0) }} step{{ (f.steps?.length ?? 0) === 1 ? '' : 's' }}
                  </span>
                </button>
              </template>
            </template>
          </div>
        </aside>

        <section class="flows-mode__detail">
          <div v-if="!draft && !selectedFlow" class="flows-mode__empty-detail">
            <p>Pick a flow on the left, or click <strong>+ New Flow</strong> above.</p>
          </div>

          <!-- Draft (new flow) -->
          <template v-else-if="draft">
            <div class="flows-detail__head">
              <div>
                <h3 class="flows-detail__title">{{ draft.name || draft.slug || '(untitled)' }}</h3>
                <div class="flows-detail__subtitle">
                  <span class="flows-detail__badge flows-detail__badge--user">user</span>
                  <code>{{ draft.slug }}</code>
                </div>
              </div>
            </div>
            <form class="flows-detail__editor" @submit.prevent="saveDraft">
              <label class="flows-detail__field">
                <span class="flows-detail__field-label">Slug</span>
                <input v-model="draft.slug" type="text" title="kebab-case, 2-40 chars" />
                <span class="flows-detail__field-hint">kebab-case, 2-40 chars</span>
              </label>
              <label class="flows-detail__field">
                <span class="flows-detail__field-label">Name</span>
                <input v-model="draft.name" type="text" />
              </label>
              <label class="flows-detail__field">
                <span class="flows-detail__field-label">Description</span>
                <input v-model="draft.description" type="text" />
              </label>

              <div class="flows-detail__field">
                <span class="flows-detail__field-label">Steps</span>
                <draggable
                  v-model="draft.steps"
                  class="flows-detail__steps"
                  :animation="120"
                  ghost-class="sortable-ghost"
                  handle=".flows-detail__step-drag"
                  item-key="__idx"
                >
                  <template #item="{ element: step, index: i }">
                    <div class="flows-detail__step">
                      <span class="flows-detail__step-drag" title="Drag to reorder">⋮⋮</span>
                      <span class="flows-detail__step-idx">{{ i + 1 }}.</span>
                      <select v-model="step.agent_slug" class="flows-detail__step-agent">
                        <option value="">(pick an agent)</option>
                        <option v-for="a in agentsCache" :key="a.slug" :value="a.slug">
                          {{ a.title }} ({{ a.slug }})
                        </option>
                      </select>
                      <label class="flows-detail__step-check">
                        <input v-model="step.optional" type="checkbox" />
                        <span>optional</span>
                      </label>
                      <button
                        type="button"
                        class="flows-detail__step-remove"
                        title="Remove step"
                        @click="removeStep(draft!, i)"
                      >✕</button>
                    </div>
                  </template>
                </draggable>
                <span class="flows-detail__field-hint">
                  Drag the handle to reorder. Tick optional for steps the flow can skip.
                </span>
                <button type="button" class="flows-detail__step-add" @click="addStep(draft)">
                  + Add step
                </button>
              </div>

              <p v-if="saveError" class="flows-detail__editor-err">{{ saveError }}</p>

              <div class="flows-detail__editor-actions">
                <button type="button" class="flows-detail__btn-ghost" @click="cancelEdit">Cancel</button>
                <button type="submit" class="flows-detail__btn-primary" :disabled="saving">
                  {{ saving ? 'Creating...' : 'Create' }}
                </button>
              </div>
            </form>
          </template>

          <!-- Selected flow -->
          <template v-else-if="selectedFlow">
            <div class="flows-detail__head">
              <div>
                <h3 class="flows-detail__title">{{ selectedFlow.name || selectedFlow.slug }}</h3>
                <div class="flows-detail__subtitle">
                  <span
                    class="flows-detail__badge"
                    :class="{ 'flows-detail__badge--user': !selectedFlow.builtin }"
                  >{{ selectedFlow.builtin ? 'built-in' : 'user' }}</span>
                  <code>{{ selectedFlow.slug }}</code>
                </div>
              </div>
              <div class="flows-detail__actions">
                <button
                  v-if="selectedFlow.builtin"
                  type="button"
                  class="flows-detail__btn-primary"
                  @click="startClone(selectedFlow)"
                >Clone</button>
                <template v-else>
                  <button
                    v-if="!editingDraft"
                    type="button"
                    class="flows-detail__btn-ghost"
                    @click="startEdit"
                  >Edit</button>
                  <button
                    type="button"
                    class="flows-detail__btn-danger"
                    @click="deleteFlow(selectedFlow.slug)"
                  >Delete</button>
                </template>
              </div>
            </div>

            <!-- Edit-in-place for user flow -->
            <form
              v-if="editingDraft && !selectedFlow.builtin"
              class="flows-detail__editor"
              @submit.prevent="saveEdit"
            >
              <label class="flows-detail__field">
                <span class="flows-detail__field-label">Slug</span>
                <input v-model="editingDraft.slug" type="text" disabled />
                <span class="flows-detail__field-hint">kebab-case, 2-40 chars</span>
              </label>
              <label class="flows-detail__field">
                <span class="flows-detail__field-label">Name</span>
                <input v-model="editingDraft.name" type="text" />
              </label>
              <label class="flows-detail__field">
                <span class="flows-detail__field-label">Description</span>
                <input v-model="editingDraft.description" type="text" />
              </label>

              <div class="flows-detail__field">
                <span class="flows-detail__field-label">Steps</span>
                <draggable
                  v-model="editingDraft.steps"
                  class="flows-detail__steps"
                  :animation="120"
                  ghost-class="sortable-ghost"
                  handle=".flows-detail__step-drag"
                  item-key="__idx"
                >
                  <template #item="{ element: step, index: i }">
                    <div class="flows-detail__step">
                      <span class="flows-detail__step-drag" title="Drag to reorder">⋮⋮</span>
                      <span class="flows-detail__step-idx">{{ i + 1 }}.</span>
                      <select v-model="step.agent_slug" class="flows-detail__step-agent">
                        <option value="">(pick an agent)</option>
                        <option v-for="a in agentsCache" :key="a.slug" :value="a.slug">
                          {{ a.title }} ({{ a.slug }})
                        </option>
                      </select>
                      <label class="flows-detail__step-check">
                        <input v-model="step.optional" type="checkbox" />
                        <span>optional</span>
                      </label>
                      <button
                        type="button"
                        class="flows-detail__step-remove"
                        title="Remove step"
                        @click="removeStep(editingDraft!, i)"
                      >✕</button>
                    </div>
                  </template>
                </draggable>
                <span class="flows-detail__field-hint">
                  Drag the handle to reorder. Tick optional for steps the flow can skip.
                </span>
                <button type="button" class="flows-detail__step-add" @click="addStep(editingDraft)">
                  + Add step
                </button>
              </div>

              <p v-if="saveError" class="flows-detail__editor-err">{{ saveError }}</p>

              <div class="flows-detail__editor-actions">
                <button type="button" class="flows-detail__btn-ghost" @click="editingDraft = null">Cancel</button>
                <button type="submit" class="flows-detail__btn-primary" :disabled="saving">
                  {{ saving ? 'Saving...' : 'Save' }}
                </button>
              </div>
            </form>

            <!-- Read-only chain -->
            <div v-else class="flows-detail__body">
              <p v-if="selectedDetail?.description" class="flows-detail__desc">
                {{ selectedDetail.description }}
              </p>
              <div v-if="(selectedDetail?.steps || []).length" class="flows-detail__chain">
                <template v-for="(group, gi) in chainGroups" :key="gi">
                  <span v-if="gi > 0" class="flows-chain__sep">→</span>
                  <template v-if="group.length === 1">
                    <button
                      type="button"
                      class="flows-chip"
                      :title="group[0].input_from
                        ? `Prompted by the output of step \&quot;${group[0].input_from}\&quot;`
                        : 'Receives the task prompt'"
                      @click="gotoAgents"
                    >{{ chipLabel(group[0]) }}</button>
                  </template>
                  <span v-else class="flows-detail__parallel">
                    <template v-for="(step, si) in group" :key="step.agent_slug + ':' + si">
                      <span v-if="si > 0" class="flows-chain__sep">‖</span>
                      <button
                        type="button"
                        class="flows-chip"
                        :title="step.input_from
                          ? `Prompted by the output of step \&quot;${step.input_from}\&quot;`
                          : 'Receives the task prompt'"
                        @click="gotoAgents"
                      >{{ chipLabel(step) }}</button>
                    </template>
                  </span>
                </template>
              </div>
            </div>
          </template>
        </section>
      </div>
    </div>
  </div>
</template>
