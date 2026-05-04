<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { api, ApiError } from '../api/client';
import { useTaskStore } from '../stores/tasks';

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

const agents = ref<Agent[]>([]);
const loading = ref(true);
const search = ref('');
const selectedSlug = ref<string | null>(null);
const draft = ref<Draft | null>(null);
const detailCache = ref<Record<string, Agent>>({});
const detailLoading = ref(false);
const saveError = ref('');
const saving = ref(false);

const defaultHarness = computed(() => store.config?.default_sandbox || 'claude');

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

const selectedAgent = computed(() => {
  if (draft.value) return null;
  if (!selectedSlug.value) return null;
  return agents.value.find((a) => a.slug === selectedSlug.value) || null;
});
const selectedDetail = computed(() => {
  if (!selectedAgent.value) return null;
  return detailCache.value[selectedAgent.value.slug] || null;
});

async function loadAgents() {
  loading.value = true;
  try {
    const rows = await api<Agent[]>('GET', '/api/agents');
    agents.value = Array.isArray(rows) ? rows : [];
    if (selectedSlug.value && !draft.value && !agents.value.find((a) => a.slug === selectedSlug.value)) {
      selectedSlug.value = null;
    }
  } catch (e) {
    console.error('agents:', e);
  } finally {
    loading.value = false;
  }
}

async function selectAgent(a: Agent) {
  draft.value = null;
  selectedSlug.value = a.slug;
  if (!detailCache.value[a.slug]) {
    detailLoading.value = true;
    try {
      const full = await api<Agent>('GET', `/api/agents/${encodeURIComponent(a.slug)}`);
      detailCache.value[a.slug] = full;
    } catch (e) {
      console.error('agent detail:', e);
    } finally {
      detailLoading.value = false;
    }
  }
}

function openNewEditor() {
  draft.value = {
    slug: 'my-agent',
    title: '',
    description: '',
    harness: '',
    multiturn: false,
    capabilities: [],
    prompt_tmpl: '',
  };
  selectedSlug.value = null;
  saveError.value = '';
}

async function startClone(role: Agent) {
  const baseDetail = detailCache.value[role.slug];
  let promptTmpl = baseDetail?.prompt_tmpl || role.prompt_tmpl || '';
  if (!promptTmpl && role.slug) {
    try {
      const full = await api<Agent>('GET', `/api/agents/${encodeURIComponent(role.slug)}`);
      detailCache.value[role.slug] = full;
      promptTmpl = full.prompt_tmpl || '';
    } catch (e) { console.warn('clone fetch:', e); }
  }
  draft.value = {
    slug: suggestCloneSlug(role.slug),
    title: role.title || '',
    description: role.description || '',
    harness: role.harness || '',
    multiturn: !!role.multiturn,
    capabilities: (role.capabilities || []).slice(),
    prompt_tmpl: promptTmpl,
  };
  selectedSlug.value = null;
  saveError.value = '';
}

function suggestCloneSlug(base: string): string {
  const s = base + '-copy';
  return s.length <= 40 ? s : base.slice(0, 35) + '-copy';
}

function cancelEdit() {
  const wasDraft = draft.value;
  draft.value = null;
  saveError.value = '';
  if (!wasDraft && selectedAgent.value && !selectedAgent.value.builtin) {
    selectedSlug.value = selectedAgent.value.slug;
  }
}

async function saveAgent() {
  if (!draft.value && !selectedAgent.value) return;
  const isCreate = !!draft.value;
  const payload = isCreate ? draft.value! : buildEditPayload();
  if (!payload) return;
  saving.value = true;
  saveError.value = '';
  try {
    const url = isCreate
      ? '/api/agents'
      : `/api/agents/${encodeURIComponent(selectedAgent.value!.slug)}`;
    const method = isCreate ? 'POST' : 'PUT';
    const saved = await api<Agent>(method, url, payload);
    draft.value = null;
    selectedSlug.value = saved.slug || payload.slug;
    delete detailCache.value[saved.slug || payload.slug];
    await loadAgents();
    if (selectedSlug.value) {
      const a = agents.value.find((x) => x.slug === selectedSlug.value);
      if (a) await selectAgent(a);
    }
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

const editingDraft = ref<Draft | null>(null);

function startEdit() {
  if (!selectedAgent.value || !selectedDetail.value) return;
  const d = selectedDetail.value;
  editingDraft.value = {
    slug: d.slug,
    title: d.title || '',
    description: d.description || '',
    harness: d.harness || '',
    multiturn: !!d.multiturn,
    capabilities: (d.capabilities || []).slice(),
    prompt_tmpl: d.prompt_tmpl || '',
  };
}

async function saveEdit() {
  if (!editingDraft.value || !selectedAgent.value) return;
  saving.value = true;
  saveError.value = '';
  try {
    await api(
      'PUT',
      `/api/agents/${encodeURIComponent(selectedAgent.value.slug)}`,
      editingDraft.value,
    );
    const slug = selectedAgent.value.slug;
    delete detailCache.value[slug];
    editingDraft.value = null;
    await loadAgents();
    const a = agents.value.find((x) => x.slug === slug);
    if (a) await selectAgent(a);
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

function buildEditPayload(): Draft | null {
  return draft.value;
}

async function deleteAgent(slug: string) {
  if (!window.confirm(`Delete agent ${slug}?`)) return;
  try {
    await api('DELETE', `/api/agents/${encodeURIComponent(slug)}`);
    selectedSlug.value = null;
    delete detailCache.value[slug];
    await loadAgents();
  } catch (e) {
    if (e instanceof ApiError) {
      window.alert('Delete failed: ' + e.message);
    } else {
      window.alert('Delete failed: ' + String(e));
    }
  }
}

const harnessOptions = [
  { value: '', label: 'Default' },
  { value: 'claude', label: 'Claude' },
  { value: 'codex', label: 'Codex' },
];
const capabilityOptions = [
  { value: 'workspace.read', label: 'workspace.read', hint: 'read workspace files' },
  { value: 'workspace.write', label: 'workspace.write', hint: 'write + commit changes' },
  { value: 'board.context', label: 'board.context', hint: 'see sibling tasks' },
];

function toggleCapability(model: Draft, value: string) {
  const i = model.capabilities.indexOf(value);
  if (i === -1) model.capabilities.push(value);
  else model.capabilities.splice(i, 1);
}

function openSandboxSettings() {
  // Settings is at /settings; the Sandbox tab is reachable via hash.
  window.location.href = '/settings#sandbox';
}

onMounted(async () => {
  if (!store.config) await store.fetchConfig();
  await loadAgents();
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
            <button type="button" class="agents-mode__new" @click="openNewEditor">
              + New Agent
            </button>
          </div>
        </div>
        <div class="agents-mode__default-row">
          <span class="agents-mode__default-label">Workspace default harness</span>
          <span class="agents-mode__default-value">{{ defaultHarness }}</span>
          <button
            type="button"
            class="agents-mode__default-edit"
            title="Change the default in Settings, Sandbox"
            @click="openSandboxSettings"
          >
            Change
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
              <p v-if="filtered.length === 0 && !draft" class="agents-mode__empty">
                {{ search ? 'No matches.' : 'No agents registered.' }}
              </p>

              <button
                v-if="draft"
                type="button"
                class="agents-rail__item agents-rail__item--draft agents-rail__item--active"
              >
                <span class="agents-rail__name">{{ draft.title || draft.slug || '(untitled)' }}</span>
                <span class="agents-rail__meta">draft</span>
              </button>

              <template v-if="builtins.length">
                <div class="agents-rail__group">Built-in</div>
                <button
                  v-for="a in builtins"
                  :key="a.slug"
                  type="button"
                  class="agents-rail__item"
                  :class="{ 'agents-rail__item--active': !draft && selectedSlug === a.slug }"
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
                  :class="{ 'agents-rail__item--active': !draft && selectedSlug === a.slug }"
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
          <div v-if="!draft && !selectedAgent" class="agents-mode__empty-detail">
            <p>Pick an agent on the left, or click <strong>+ New Agent</strong> above.</p>
          </div>

          <!-- Draft / new agent editor -->
          <template v-else-if="draft">
            <div class="agents-detail__head">
              <div>
                <h3 class="agents-detail__title">{{ draft.title || draft.slug || '(untitled)' }}</h3>
                <div class="agents-detail__subtitle">
                  <span class="agents-detail__badge agents-detail__badge--user">user</span>
                  <code>{{ draft.slug }}</code>
                </div>
              </div>
            </div>
            <form class="agents-detail__editor" @submit.prevent="saveAgent">
              <label class="agents-detail__field">
                <span class="agents-detail__field-label">Slug</span>
                <input v-model="draft.slug" type="text" title="kebab-case, 2-40 chars" />
                <span class="agents-detail__field-hint">kebab-case, 2-40 chars</span>
              </label>
              <label class="agents-detail__field">
                <span class="agents-detail__field-label">Title</span>
                <input v-model="draft.title" type="text" />
              </label>
              <label class="agents-detail__field">
                <span class="agents-detail__field-label">Description</span>
                <input v-model="draft.description" type="text" />
              </label>

              <div class="agents-detail__field">
                <span class="agents-detail__field-label">Harness</span>
                <div class="agents-detail__segment">
                  <button
                    v-for="opt in harnessOptions"
                    :key="opt.value"
                    type="button"
                    class="agents-detail__segment-btn"
                    :class="{ 'agents-detail__segment-btn--active': draft.harness === opt.value }"
                    @click="draft.harness = opt.value"
                  >{{ opt.label }}</button>
                </div>
                <span class="agents-detail__field-hint">
                  Default inherits from the workspace setting.
                  Claude and Codex pin this agent to a specific harness regardless of task or env config.
                </span>
              </div>

              <div class="agents-detail__field">
                <span class="agents-detail__field-label">Capabilities</span>
                <div class="agents-detail__checks">
                  <label v-for="cap in capabilityOptions" :key="cap.value" class="agents-detail__check">
                    <input
                      type="checkbox"
                      :checked="draft.capabilities.includes(cap.value)"
                      @change="toggleCapability(draft, cap.value)"
                    />
                    <span :title="cap.hint">{{ cap.label }}</span>
                  </label>
                </div>
              </div>

              <label class="agents-detail__field agents-detail__field--check">
                <input v-model="draft.multiturn" type="checkbox" />
                <span>Multi-turn</span>
                <span class="agents-detail__field-hint">
                  Advisory only: the runner's binding table is the source of truth for dispatch.
                </span>
              </label>

              <div class="agents-detail__field agents-detail__field--prompt">
                <span class="agents-detail__field-label">System Prompt</span>
                <textarea v-model="draft.prompt_tmpl" rows="14" name="prompt_tmpl"></textarea>
                <span class="agents-detail__field-hint">
                  Optional preamble prepended to every invocation of this agent
                  through the flow engine. The agent sees this text first, then
                  a blank line, then the caller's prompt. Leave empty to use the
                  agent's default behaviour. Note: built-in sub-agents invoked by
                  the implement turn loop (title, oversight, commit-msg) use
                  their embedded templates regardless; put custom prompts on a
                  clone referenced from a custom flow.
                </span>
              </div>

              <p v-if="saveError" class="agents-detail__editor-err">{{ saveError }}</p>

              <div class="agents-detail__editor-actions">
                <button type="button" class="agents-detail__btn-ghost" @click="cancelEdit">Cancel</button>
                <button type="submit" class="agents-detail__btn-primary" :disabled="saving">
                  {{ saving ? 'Creating...' : 'Create' }}
                </button>
              </div>
            </form>
          </template>

          <!-- Selected agent (built-in: read-only; user: edit-in-place) -->
          <template v-else-if="selectedAgent">
            <div class="agents-detail__head">
              <div>
                <h3 class="agents-detail__title">{{ selectedAgent.title || selectedAgent.slug }}</h3>
                <div class="agents-detail__subtitle">
                  <span
                    class="agents-detail__badge"
                    :class="{ 'agents-detail__badge--user': !selectedAgent.builtin }"
                  >{{ selectedAgent.builtin ? 'built-in' : 'user' }}</span>
                  <code>{{ selectedAgent.slug }}</code>
                </div>
              </div>
              <div class="agents-detail__actions">
                <button
                  v-if="selectedAgent.builtin"
                  type="button"
                  class="agents-detail__btn-primary"
                  @click="startClone(selectedAgent)"
                >Clone</button>
                <template v-else>
                  <button
                    v-if="!editingDraft"
                    type="button"
                    class="agents-detail__btn-ghost"
                    @click="startEdit"
                  >Edit</button>
                  <button
                    type="button"
                    class="agents-detail__btn-danger"
                    @click="deleteAgent(selectedAgent.slug)"
                  >Delete</button>
                </template>
              </div>
            </div>

            <!-- User edit-in-place -->
            <form
              v-if="editingDraft && !selectedAgent.builtin"
              class="agents-detail__editor"
              @submit.prevent="saveEdit"
            >
              <label class="agents-detail__field">
                <span class="agents-detail__field-label">Slug</span>
                <input v-model="editingDraft.slug" type="text" disabled />
                <span class="agents-detail__field-hint">kebab-case, 2-40 chars</span>
              </label>
              <label class="agents-detail__field">
                <span class="agents-detail__field-label">Title</span>
                <input v-model="editingDraft.title" type="text" />
              </label>
              <label class="agents-detail__field">
                <span class="agents-detail__field-label">Description</span>
                <input v-model="editingDraft.description" type="text" />
              </label>
              <div class="agents-detail__field">
                <span class="agents-detail__field-label">Harness</span>
                <div class="agents-detail__segment">
                  <button
                    v-for="opt in harnessOptions"
                    :key="opt.value"
                    type="button"
                    class="agents-detail__segment-btn"
                    :class="{ 'agents-detail__segment-btn--active': editingDraft.harness === opt.value }"
                    @click="editingDraft.harness = opt.value"
                  >{{ opt.label }}</button>
                </div>
              </div>
              <div class="agents-detail__field">
                <span class="agents-detail__field-label">Capabilities</span>
                <div class="agents-detail__checks">
                  <label v-for="cap in capabilityOptions" :key="cap.value" class="agents-detail__check">
                    <input
                      type="checkbox"
                      :checked="editingDraft.capabilities.includes(cap.value)"
                      @change="toggleCapability(editingDraft, cap.value)"
                    />
                    <span :title="cap.hint">{{ cap.label }}</span>
                  </label>
                </div>
              </div>
              <label class="agents-detail__field agents-detail__field--check">
                <input v-model="editingDraft.multiturn" type="checkbox" />
                <span>Multi-turn</span>
              </label>
              <div class="agents-detail__field agents-detail__field--prompt">
                <span class="agents-detail__field-label">System Prompt</span>
                <textarea v-model="editingDraft.prompt_tmpl" rows="14" name="prompt_tmpl"></textarea>
              </div>
              <p v-if="saveError" class="agents-detail__editor-err">{{ saveError }}</p>
              <div class="agents-detail__editor-actions">
                <button type="button" class="agents-detail__btn-ghost" @click="editingDraft = null">Cancel</button>
                <button type="submit" class="agents-detail__btn-primary" :disabled="saving">
                  {{ saving ? 'Saving...' : 'Save' }}
                </button>
              </div>
            </form>

            <!-- Read-only body for built-in (or user when not editing) -->
            <div v-else class="agents-detail__body">
              <div class="agents-detail__kv">
                <span class="agents-detail__kv-key">Description</span>
                <span class="agents-detail__kv-value">{{ selectedAgent.description || '' }}</span>
              </div>
              <div class="agents-detail__kv">
                <span class="agents-detail__kv-key">Harness</span>
                <span class="agents-detail__kv-value">{{ selectedAgent.harness || '(use workspace default)' }}</span>
              </div>
              <div class="agents-detail__kv">
                <span class="agents-detail__kv-key">Capabilities</span>
                <span class="agents-detail__kv-value">{{ (selectedAgent.capabilities || []).join(', ') || '(none)' }}</span>
              </div>
              <div class="agents-detail__kv">
                <span class="agents-detail__kv-key">Turn model</span>
                <span class="agents-detail__kv-value">{{ selectedAgent.multiturn ? 'multi-turn' : 'single-turn' }}</span>
              </div>

              <div class="agents-detail__section">
                <div class="agents-detail__section-label">System prompt</div>
                <pre v-if="detailLoading" class="agents-detail__tmpl">Loading...</pre>
                <pre
                  v-else-if="selectedDetail?.prompt_tmpl"
                  class="agents-detail__tmpl"
                >{{ selectedDetail.prompt_tmpl }}</pre>
                <pre v-else class="agents-detail__tmpl agents-detail__tmpl--empty">(no system prompt; the agent consumes the task prompt directly)</pre>
              </div>
            </div>
          </template>
        </section>
      </div>
    </div>
  </div>
</template>
