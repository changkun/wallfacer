<script setup lang="ts">
import { ref, computed, watch } from 'vue';
import { api, ApiError } from '../api/client';
import { useTaskStore } from '../stores/tasks';
import { useDialogStore } from '../stores/dialog';
import { supportedHarnesses, harnessLabel } from '../lib/harness';

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

const props = defineProps<{
  // When isNew is false, the selected agent being viewed/edited.
  // When isNew is true, the optional clone source (null = blank new agent).
  agent: Agent | null;
  isNew: boolean;
}>();

const emit = defineEmits<{
  (e: 'saved', slug: string): void;
  (e: 'deleted', slug: string): void;
  (e: 'cancel'): void;
  (e: 'clone', agent: Agent): void;
}>();

// The new-agent / clone draft. Local component state (not a v-model): the
// embedding page mounts this editor as a modal and reads results via the
// `saved`/`deleted` events, so a plain ref is what drives the form. A
// defineModel without a parent binding does not deep-react to nested writes
// (draft.harness = ...), which silently froze the harness segmented control.
// All seeding (including the async clone fill) happens inside this component.
const draft = ref<Draft | null>(null);

const store = useTaskStore();
const dialog = useDialogStore();

const editingDraft = ref<Draft | null>(null);
const detailCache = ref<Record<string, Agent>>({});
const detailLoading = ref(false);
const saveError = ref('');
const saving = ref(false);

const selectedDetail = computed(() => {
  if (props.isNew || !props.agent) return null;
  return detailCache.value[props.agent.slug] || null;
});

// Supported harnesses advertised by the server (falls back to the full
// registry before config loads), used to populate the harness picker.
const harnessChoices = computed(() => supportedHarnesses(store.config?.sandboxes));

const harnessOptions = computed(() => [
  { value: '', label: 'Default' },
  ...harnessChoices.value.map((id) => ({ value: id, label: harnessLabel(id) })),
]);
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

function seedDraft(d: Agent): Draft {
  return {
    slug: d.slug,
    title: d.title || '',
    description: d.description || '',
    harness: d.harness || '',
    multiturn: !!d.multiturn,
    capabilities: (d.capabilities || []).slice(),
    prompt_tmpl: d.prompt_tmpl || '',
  };
}

function suggestCloneSlug(base: string): string {
  const s = base + '-copy';
  return s.length <= 40 ? s : base.slice(0, 35) + '-copy';
}

// Seed the editor whenever the inputs change. A token guards async re-seeds so
// a slow detail/prompt fetch never overwrites a newer selection.
let seedToken = 0;
async function seedFromProps() {
  const token = ++seedToken;
  if (props.isNew) {
    // New / clone draft mode: clearing saveError matches the original
    // openNewEditor/startClone behaviour.
    saveError.value = '';
    editingDraft.value = null;
    const seed = props.agent;
    if (!seed) {
      draft.value = {
        slug: 'my-agent',
        title: '',
        description: '',
        harness: '',
        multiturn: false,
        capabilities: [],
        prompt_tmpl: '',
      };
      return;
    }
    // Clone: seed synchronously so the form renders immediately, then patch
    // prompt_tmpl from the full detail if the list row did not carry it.
    draft.value = {
      slug: suggestCloneSlug(seed.slug),
      title: seed.title || '',
      description: seed.description || '',
      harness: seed.harness || '',
      multiturn: !!seed.multiturn,
      capabilities: (seed.capabilities || []).slice(),
      prompt_tmpl: detailCache.value[seed.slug]?.prompt_tmpl || seed.prompt_tmpl || '',
    };
    if (!draft.value.prompt_tmpl && seed.slug) {
      try {
        const full = await api<Agent>('GET', `/api/agents/${encodeURIComponent(seed.slug)}`);
        detailCache.value[seed.slug] = full;
        if (token === seedToken && draft.value) draft.value.prompt_tmpl = full.prompt_tmpl || '';
      } catch (e) {
        console.warn('clone fetch:', e);
      }
    }
    return;
  }

  // Selected-agent mode (built-in read-only, or user edit-in-place).
  draft.value = null;
  const a = props.agent;
  if (!a) {
    editingDraft.value = null;
    return;
  }
  // User-authored agents edit in place. Seed synchronously from the list row
  // so the editor renders immediately, then refresh from the full detail
  // (which carries prompt_tmpl) once it resolves.
  const slug = a.slug;
  const cached = !!detailCache.value[slug];
  editingDraft.value = a.builtin ? null : seedDraft(detailCache.value[slug] || a);
  if (!cached) {
    detailLoading.value = true;
    try {
      const full = await api<Agent>('GET', `/api/agents/${encodeURIComponent(slug)}`);
      detailCache.value[slug] = full;
    } catch (e) {
      console.error('agent detail:', e);
    } finally {
      detailLoading.value = false;
    }
    // Refresh the edit form with the full detail (carries prompt_tmpl).
    if (token === seedToken && props.agent?.slug === slug && !a.builtin && detailCache.value[slug]) {
      editingDraft.value = seedDraft(detailCache.value[slug]);
    }
  }
}

watch(() => [props.agent, props.isNew], seedFromProps, { immediate: true });

async function saveAgent() {
  if (!draft.value) return;
  const payload = draft.value;
  saving.value = true;
  saveError.value = '';
  try {
    const saved = await api<Agent>('POST', '/api/agents', payload);
    const slug = saved.slug || payload.slug;
    delete detailCache.value[slug];
    draft.value = null;
    emit('saved', slug);
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

function cancelEdit() {
  draft.value = null;
  saveError.value = '';
  emit('cancel');
}

function cancelUserEdit() {
  // Revert edits and stay in the editor (there is no read-only user view).
  if (selectedDetail.value) editingDraft.value = seedDraft(selectedDetail.value);
  saveError.value = '';
}

async function saveEdit() {
  if (!editingDraft.value || !props.agent) return;
  saving.value = true;
  saveError.value = '';
  try {
    await api(
      'PUT',
      `/api/agents/${encodeURIComponent(props.agent.slug)}`,
      editingDraft.value,
    );
    const slug = props.agent.slug;
    delete detailCache.value[slug];
    emit('saved', slug);
  } catch (e) {
    saveError.value = e instanceof Error ? e.message : String(e);
  } finally {
    saving.value = false;
  }
}

async function deleteAgent(slug: string) {
  const ok = await dialog.confirm({
    title: 'Delete agent',
    message: `Delete agent ${slug}?`,
    confirmLabel: 'Delete',
    danger: true,
  });
  if (!ok) return;
  try {
    await api('DELETE', `/api/agents/${encodeURIComponent(slug)}`);
    delete detailCache.value[slug];
    emit('deleted', slug);
  } catch (e) {
    await dialog.alert('Delete failed: ' + (e instanceof ApiError ? e.message : String(e)));
  }
}
</script>

<template>
  <!-- Draft / new agent editor -->
  <template v-if="isNew && draft">
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
  <template v-else-if="!isNew && agent">
    <div class="agents-detail__head">
      <div>
        <h3 class="agents-detail__title">{{ agent.title || agent.slug }}</h3>
        <div class="agents-detail__subtitle">
          <span
            class="agents-detail__badge"
            :class="{ 'agents-detail__badge--user': !agent.builtin }"
          >{{ agent.builtin ? 'built-in' : 'user' }}</span>
          <code>{{ agent.slug }}</code>
        </div>
      </div>
      <div class="agents-detail__actions">
        <button
          v-if="agent.builtin"
          type="button"
          class="agents-detail__btn-primary"
          @click="emit('clone', agent)"
        >Clone</button>
        <button
          v-else
          type="button"
          class="agents-detail__btn-danger"
          @click="deleteAgent(agent.slug)"
        >Delete</button>
      </div>
    </div>

    <!-- User edit-in-place -->
    <form
      v-if="editingDraft && !agent.builtin"
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
        <span class="agents-detail__field-hint">
          Advisory only: the runner's binding table is the source of truth for dispatch.
        </span>
      </label>
      <div class="agents-detail__field agents-detail__field--prompt">
        <span class="agents-detail__field-label">System Prompt</span>
        <textarea v-model="editingDraft.prompt_tmpl" rows="14" name="prompt_tmpl"></textarea>
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
        <button type="button" class="agents-detail__btn-ghost" @click="cancelUserEdit">Cancel</button>
        <button type="submit" class="agents-detail__btn-primary" :disabled="saving">
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
      </div>
    </form>

    <!-- Read-only body for built-in (or user when not editing) -->
    <div v-else class="agents-detail__body">
      <div class="agents-detail__kv">
        <span class="agents-detail__kv-key">Description</span>
        <span class="agents-detail__kv-value">{{ agent.description || '' }}</span>
      </div>
      <div class="agents-detail__kv">
        <span class="agents-detail__kv-key">Harness</span>
        <span class="agents-detail__kv-value">{{ agent.harness || '(use workspace default)' }}</span>
      </div>
      <div class="agents-detail__kv">
        <span class="agents-detail__kv-key">Capabilities</span>
        <span class="agents-detail__kv-value">{{ (agent.capabilities || []).join(', ') || '(none)' }}</span>
      </div>
      <div class="agents-detail__kv">
        <span class="agents-detail__kv-key">Turn model</span>
        <span class="agents-detail__kv-value">{{ agent.multiturn ? 'multi-turn' : 'single-turn' }}</span>
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
</template>
