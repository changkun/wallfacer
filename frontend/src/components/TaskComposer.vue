<script setup lang="ts">
import { nextTick, ref, computed, onMounted, watch } from 'vue';
import { useTaskStore } from '../stores/tasks';
import { api } from '../api/client';
import { splitBatch } from '../lib/composer';
import { useMentions } from '../composables/useMentions';
import DependencyPicker from './DependencyPicker.vue';
import HarnessSelect from './HarnessSelect.vue';
import AppSelect from './AppSelect.vue';
import { getStored, setStored, removeStored } from '../lib/storage';
import { coordinationOf, type Coordination } from '../lib/flowDraft';
import type { FlowTopology } from '../api/types';

// FlowOption mirrors the agent-graph (flow) list from GET /api/flows. The
// coordination fields let the composer show how the chosen graph runs, and warn
// when it is a delegating (experimental) one.
interface FlowOption {
  slug: string;
  name: string;
  agentic?: boolean;
  dynamic?: boolean;
  topology?: FlowTopology;
}

const props = defineProps<{ autoExpand?: boolean }>();
const store = useTaskStore();

// Composer draft persistence — losing typing on a stray refresh is a
// real UX regression vs the legacy UI, which used wallfacer-new-task-draft.
// Same key so users mid-migration don't lose work.
const DRAFT_KEY = 'wallfacer-new-task-draft';
const prompt = ref<string>(getStored(DRAFT_KEY) ?? '');
watch(prompt, (v) => {
  if (v.trim()) setStored(DRAFT_KEY, v);
  else removeStored(DRAFT_KEY);
});
const mentions = useMentions({ setValue: (v) => { prompt.value = v; }, priorityPrefix: 'spec/' });
const submitting = ref(false);
const expanded = ref(false);
const textareaRef = ref<HTMLTextAreaElement | null>(null);
const modKey = typeof navigator !== 'undefined' && /Mac/.test(navigator.platform) ? '⌘' : 'Ctrl';

// Advanced fields.
const flows = ref<FlowOption[]>([]);
const flow = ref('implement');
// Tag chips: committed tags + a pending draft. comma/Enter commit the draft;
// Backspace on an empty draft removes the last chip. Mirrors legacy tasks.js.
const tags = ref<string[]>([]);
const tagDraft = ref('');
function commitTag() {
  const raw = tagDraft.value.trim().replace(/,$/, '').trim();
  tagDraft.value = '';
  if (raw && !tags.value.includes(raw)) tags.value.push(raw);
}
function onTagKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter' || e.key === ',') { e.preventDefault(); commitTag(); return; }
  if (e.key === 'Backspace' && tagDraft.value === '' && tags.value.length) {
    e.preventDefault();
    tags.value.pop();
  }
}
function removeTag(i: number) { tags.value.splice(i, 1); }
// A trailing comma typed/pasted into the draft auto-commits.
watch(tagDraft, (v) => { if (v.includes(',')) { tagDraft.value = v.replace(/,/g, ''); commitTag(); } });

// Timeout presets for the select; 'custom' reveals a minutes field.
const TIMEOUT_OPTIONS = [
  { value: '', label: 'Default' },
  { value: '15', label: '15 min' },
  { value: '30', label: '30 min' },
  { value: '60', label: '1 hour' },
  { value: '120', label: '2 hours' },
  { value: '300', label: '5 hours' },
  { value: 'custom', label: 'Custom…' },
];
const timeoutPreset = ref<'' | '15' | '30' | '60' | '120' | '300' | 'custom'>('');
const timeoutCustomMin = ref<number | null>(null);
const timeoutMin = computed<number | null>(() => {
  if (!timeoutPreset.value) return null;
  if (timeoutPreset.value === 'custom') return timeoutCustomMin.value;
  return Number(timeoutPreset.value);
});

// Flow-aware placeholder hint, mirroring the legacy data-task-flow behavior.
const flowOptions = computed(() => flows.value.map((f) => ({ value: f.slug, label: f.name })));

// Coordination of the selected agent graph: how it runs a task. Delegating modes
// (lead / mesh) are experimental (no durable commits yet), so the composer warns.
const selectedFlowOption = computed(() => flows.value.find((f) => f.slug === flow.value));
const selectedCoordination = computed<Coordination>(() =>
  coordinationOf(selectedFlowOption.value ?? {}),
);
const coordinationLabel = computed(
  () =>
    ({ sequence: 'Fixed sequence', lead: 'Lead delegates', mesh: 'Open mesh' })[
      selectedCoordination.value
    ],
);
const coordinationExperimental = computed(() => selectedCoordination.value !== 'sequence');
const promptPlaceholder = computed(() => {
  const f = flow.value || 'implement';
  return `Describe the task… (graph: ${f} · Markdown, @ to mention files, ${modKey}↵ to save)`;
});
// Optional overrides (behind a "More" toggle).
const showMore = ref(false);
const model = ref('');
const criteria = ref('');
const maxCostUsd = ref<number | null>(null);
const maxInputTokens = ref<number | null>(null);
const dependsOn = ref<string[]>([]);
const sandbox = ref<'' | 'claude' | 'codex' | 'cursor' | 'pi' | 'opencode' | 'topos'>('');
// Harness options for the picker: prefer the server's registered list so a
// newly added harness appears without editing this component; fall back to
// the known set before config loads.
const harnessOptions = computed<string[]>(() =>
  store.config?.sandboxes?.length
    ? store.config.sandboxes
    : ['claude', 'codex', 'cursor', 'pi', 'opencode', 'topos'],
);
function onHarnessChange(v: string): void {
  sandbox.value = v as typeof sandbox.value;
}
const batchMode = ref(false);
const scheduled = ref(false);
const intervalMinutes = ref<number | null>(null);

// Candidate dependencies: existing non-archived tasks (most recent first).
const depCandidates = computed(() =>
  store.tasks
    .filter((t) => !t.archived && t.kind !== 'routine')
    .map((t) => ({ id: t.id, label: (t.title || t.prompt || t.id).slice(0, 60) })),
);

const batchCount = computed(() => (batchMode.value ? splitBatch(prompt.value).length : 0));

async function loadFlows() {
  if (flows.value.length) return;
  try {
    const res = await api<FlowOption[] | { flows: FlowOption[] }>('GET', '/api/flows');
    flows.value = Array.isArray(res) ? res : (res?.flows ?? []);
  } catch (e) {
    console.error('load flows:', e);
  }
}

// Inserts an "@" at the cursor (with a leading space if needed) and opens the
// mention autocomplete, matching the legacy board-composer "@" action button.
function insertAtMention() {
  const el = textareaRef.value;
  if (!el) return;
  const pos = el.selectionStart ?? el.value.length;
  const before = el.value.slice(0, pos);
  const after = el.value.slice(pos);
  const insert = before.length > 0 && !/\s$/.test(before) ? ' @' : '@';
  prompt.value = before + insert + after;
  nextTick(() => {
    const newPos = pos + insert.length;
    el.focus();
    el.setSelectionRange(newPos, newPos);
    void mentions.onInput(el);
  });
}

// When `autoExpand` is passed (typically by the BoardPage empty state),
// open the composer on mount so the user sees the prompt textarea first.
onMounted(() => {
  // Restoring a saved draft is just as much a signal to expand as an
  // explicit autoExpand prop — surface the work the user already typed.
  if (props.autoExpand || prompt.value.trim()) void expand();
});

async function expand() {
  expanded.value = true;
  loadFlows();
  await nextTick();
  textareaRef.value?.focus();
}

function collapse() {
  expanded.value = false;
  prompt.value = '';
  tags.value = []; tagDraft.value = '';
  timeoutPreset.value = '';
  timeoutCustomMin.value = null;
  showMore.value = false;
  model.value = '';
  criteria.value = '';
  maxCostUsd.value = null;
  maxInputTokens.value = null;
  dependsOn.value = [];
  sandbox.value = '';
  batchMode.value = false;
  scheduled.value = false;
  intervalMinutes.value = null;
}

async function submitRoutine(text: string): Promise<void> {
  const minutes = intervalMinutes.value;
  if (!minutes || minutes <= 0) return;
  await api('POST', '/api/routines', {
    prompt: text,
    interval_minutes: minutes,
    spawn_flow: flow.value || 'implement',
    timeout: timeoutMin.value && timeoutMin.value > 0 ? timeoutMin.value : undefined,
    tags: tags.value.slice(),
  });
}

async function submit() {
  const text = prompt.value.trim();
  // A prompt is always required; bail on empty input or an in-flight submit.
  if (!text || submitting.value) return;
  submitting.value = true;
  // Drop the persisted draft up front: when the empty-state composer creates
  // the first task, the board re-renders and mounts the in-backlog composer,
  // which would otherwise read this draft and reopen prefilled. Restored on
  // failure so a network error doesn't lose the user's text.
  removeStored(DRAFT_KEY);
  try {
    if (scheduled.value) {
      await submitRoutine(text);
      collapse();
      return;
    }
    const sharedOpts = {
      flow: flow.value || undefined,
      criteria: criteria.value.trim() || undefined,
      tags: tags.value.slice(),
      timeout: timeoutMin.value && timeoutMin.value > 0 ? timeoutMin.value : undefined,
      model: model.value.trim() || undefined,
      maxCostUsd: maxCostUsd.value ?? undefined,
      maxInputTokens: maxInputTokens.value ?? undefined,
    } as const;

    if (batchMode.value) {
      const prompts = splitBatch(text);
      if (prompts.length === 0) return;
      const res = await store.batchCreateTasks(prompts, sharedOpts);
      const created = res?.tasks ?? [];
      // Apply per-task sandbox / shared dependsOn via follow-up PATCH.
      if (sandbox.value || dependsOn.value.length) {
        const patch: Record<string, unknown> = {};
        if (dependsOn.value.length) patch.depends_on = [...dependsOn.value];
        if (sandbox.value) patch.sandbox = sandbox.value;
        await Promise.all(
          created.filter((t) => t.id).map((t) => store.patchTask(t.id, { ...patch })),
        );
      }
    } else {
      const created = await store.createTask(text, sharedOpts);
      if (created?.id) {
        const patch: Record<string, unknown> = {};
        if (dependsOn.value.length) patch.depends_on = [...dependsOn.value];
        // POST /api/tasks rejects sandbox; the server-side path for per-task
        // sandbox overrides is a follow-up PATCH (see docs/internals/api-and-transport.md).
        if (sandbox.value) patch.sandbox = sandbox.value;
        if (Object.keys(patch).length) {
          await store.patchTask(created.id, patch);
        }
      }
    }

    prompt.value = '';
    tags.value = []; tagDraft.value = '';
    timeoutPreset.value = '';
    timeoutCustomMin.value = null;
    model.value = '';
    maxCostUsd.value = null;
    maxInputTokens.value = null;
    dependsOn.value = [];
    sandbox.value = '';
    batchMode.value = false;
    expanded.value = false;
  } catch (e) {
    console.error('create task:', e);
    // Re-persist the draft so a failed create doesn't lose the user's text.
    if (text) setStored(DRAFT_KEY, prompt.value || text);
  } finally {
    submitting.value = false;
  }
}

function onKeydown(e: KeyboardEvent) {
  // The mention dropdown gets first crack at arrows / Enter / Tab / Escape.
  if (mentions.onKeydown(e, e.target as HTMLTextAreaElement)) return;
  if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
    e.preventDefault();
    submit();
    return;
  }
  if (e.key === 'Escape') {
    e.preventDefault();
    collapse();
  }
}

function onInput(e: Event) {
  mentions.onInput(e.target as HTMLTextAreaElement);
}
</script>

<template>
  <button
    v-if="!expanded"
    type="button"
    class="btn ghost block composer-add"
    @click="expand"
  >
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><line x1="12" y1="5" x2="12" y2="19"></line><line x1="5" y1="12" x2="19" y2="12"></line></svg>
    New task
  </button>
  <form v-else class="composer" @submit.prevent="submit">
    <div class="composer__prompt-wrap">
      <textarea
        ref="textareaRef"
        v-model="prompt"
        class="field composer__prompt"
        :placeholder="promptPlaceholder"
        rows="4"
        @keydown="onKeydown"
        @input="onInput"
      />
      <ul v-if="mentions.open.value" class="composer__mentions" role="listbox">
        <li
          v-for="(file, i) in mentions.items.value"
          :key="file"
          class="composer__mention"
          :class="{ active: i === mentions.activeIndex.value }"
          role="option"
          :aria-selected="i === mentions.activeIndex.value"
          @mousedown.prevent="mentions.choose(textareaRef!, file)"
          @mouseenter="mentions.activeIndex.value = i"
        >{{ file }}</li>
      </ul>
    </div>
    <div class="composer__opts">
      <label class="composer__opt">
        <span class="composer__opt-label">Agent graph</span>
        <AppSelect v-model="flow" :options="flowOptions" aria-label="Agent graph" block />
        <span
          v-if="flow"
          class="composer__coord"
          :class="{ 'composer__coord--experimental': coordinationExperimental }"
          :title="coordinationExperimental
            ? 'Delegating graphs are experimental: no durable commits yet. Use a Fixed sequence graph for real runs.'
            : 'Runs the graph\'s agents in a fixed order.'"
        >{{ coordinationLabel }}<template v-if="coordinationExperimental"> · experimental</template></span>
      </label>
      <label class="composer__opt composer__opt--grow">
        <span class="composer__opt-label">Tags</span>
        <div class="composer__tags">
          <span v-for="(tag, i) in tags" :key="tag" class="composer__tag-chip">
            {{ tag }}
            <button type="button" class="composer__tag-x" title="Remove tag" @click="removeTag(i)">×</button>
          </span>
          <input
            v-model="tagDraft"
            class="composer__tag-input"
            type="text"
            :placeholder="tags.length ? '' : 'tag, tag…'"
            aria-label="Add tag"
            @keydown="onTagKeydown"
            @blur="commitTag"
          />
        </div>
      </label>
      <label class="composer__opt">
        <span class="composer__opt-label">Timeout</span>
        <div class="composer__opt-controls">
          <AppSelect v-model="timeoutPreset" :options="TIMEOUT_OPTIONS" aria-label="Timeout preset" class="composer__select" />
          <input
            v-if="timeoutPreset === 'custom'"
            v-model.number="timeoutCustomMin"
            class="field composer__input composer__input--num"
            type="number"
            min="1"
            placeholder="min"
            aria-label="Custom timeout in minutes"
          />
        </div>
      </label>
      <div class="composer__utils">
        <button
          type="button"
          class="btn sm ghost composer__more"
          title="Mention a file (@)"
          aria-label="Insert @ mention"
          @click="insertAtMention"
        >@</button>
        <button type="button" class="btn sm ghost composer__more" @click="showMore = !showMore">
          {{ showMore ? '− Less' : '+ More' }}
        </button>
      </div>
    </div>
    <div v-if="showMore" class="composer__opts">
      <label class="composer__opt composer__opt--grow">
        <span class="composer__opt-label">Test criteria</span>
        <input v-model="criteria" class="field composer__input" type="text" placeholder="what the test agent should verify (optional)" aria-label="Test criteria" />
      </label>
      <label class="composer__opt composer__opt--grow">
        <span class="composer__opt-label">Model</span>
        <input v-model="model" class="field composer__input" type="text" placeholder="override model" aria-label="Model override" />
      </label>
      <label class="composer__opt">
        <span class="composer__opt-label">Max $</span>
        <input v-model.number="maxCostUsd" class="field composer__input composer__input--num" type="number" min="0" step="0.5" placeholder="USD" aria-label="Max cost USD" />
      </label>
      <label class="composer__opt">
        <span class="composer__opt-label">Max tokens</span>
        <input v-model.number="maxInputTokens" class="field composer__input composer__input--num" type="number" min="0" step="1000" placeholder="input" aria-label="Max input tokens" />
      </label>
      <div v-if="depCandidates.length" class="composer__opt composer__opt--grow">
        <span class="composer__opt-label">Depends on</span>
        <DependencyPicker v-model="dependsOn" />
      </div>
      <div class="composer__opt">
        <span class="composer__opt-label">Harness</span>
        <HarnessSelect
          :model-value="sandbox"
          :options="harnessOptions"
          @update:model-value="onHarnessChange"
        />
      </div>
    </div>
    <div class="composer__actions">
      <label class="composer__toggle">
        <input v-model="batchMode" type="checkbox" :disabled="scheduled" />
        Batch mode
        <span v-if="batchMode" class="composer__toggle-hint">
          blank-line separated · {{ batchCount }} task{{ batchCount === 1 ? '' : 's' }}
        </span>
      </label>
      <label class="composer__toggle">
        <input v-model="scheduled" type="checkbox" :disabled="batchMode" />
        Schedule
        <input
          v-if="scheduled"
          v-model.number="intervalMinutes"
          class="field composer__input composer__input--num"
          type="number"
          min="1"
          placeholder="min"
          aria-label="Interval minutes"
        />
      </label>
      <span class="composer__spacer" />
      <div class="composer__btn-group">
        <button
          type="button"
          class="btn sm ghost"
          @click="collapse"
        >
          Cancel
        </button>
        <button
          type="submit"
          class="btn sm"
          :disabled="
            !prompt.trim() ||
            submitting ||
            (batchMode && batchCount === 0) ||
            (scheduled && (!intervalMinutes || intervalMinutes <= 0))
          "
        >
          {{
            submitting
              ? 'Saving…'
              : scheduled
                ? 'Schedule'
                : batchMode
                  ? `Save ${batchCount}`
                  : 'Save'
          }}
        </button>
      </div>
    </div>
  </form>
</template>

<style scoped>
/* The composer is a card: the prompt sits borderless inside it, the options
   are fields, and the actions row is its foot. */
.composer {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px 10px;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-lg);
  box-shadow: var(--sh-card);
  font-size: var(--fs-10);
  color: var(--ink-2);
  transition: border-color 160ms var(--ease-fluid), box-shadow 160ms var(--ease-fluid);
}
.composer:focus-within {
  border-color: var(--accent-line);
  box-shadow: var(--sh-2);
}
.composer-add {
  gap: 6px;
  color: var(--ink-3);
  border-style: dashed;
  background: transparent;
}
.composer-add:hover {
  color: var(--accent);
}
.composer__prompt-wrap { position: relative; }
/* The prompt is a bounded field like every other control in the card. */
.composer__prompt {
  min-height: 84px;
  resize: vertical;
  font-size: var(--fs-md);
  line-height: 1.45;
}
.composer__prompt::placeholder {
  color: var(--ink-4);
}
.composer__mentions {
  position: absolute;
  left: 0;
  right: 0;
  top: 100%;
  z-index: 30;
  margin: 2px 0 0;
  padding: 4px;
  list-style: none;
  max-height: 220px;
  overflow-y: auto;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-lg);
  box-shadow: var(--sh-pop);
}
.composer__mention {
  padding: 5px 8px;
  font-size: 12px;
  font-family: var(--font-mono);
  border-radius: var(--r-sm);
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.composer__mention.active,
.composer__mention:hover { background: var(--bg-sunk); }
.composer__opts {
  display: flex;
  gap: 8px 10px;
  padding-top: 8px;
  border-top: 1px solid var(--rule);
  /* Align every option at the top so labels line up and each control sits at
     the same y, whatever a sublabel below it does. */
  align-items: flex-start;
  flex-wrap: wrap;
}
.composer__opt {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
}
/* Growable text fields share one flex basis and a max width so no single field
   balloons far wider than its neighbours when a row wraps. */
.composer__opt--grow { flex: 1 1 200px; min-width: 150px; max-width: 380px; }
/* The timeout preset select and its "Custom…" minutes input stay on one row. */
.composer__opt-controls { display: flex; align-items: center; gap: 4px; }
.composer__opt-label {
  font: 600 var(--fs-9) / 1 var(--font-mono);
  letter-spacing: var(--tracking-label);
  text-transform: uppercase;
  color: var(--ink-3);
  padding-top: 2px;
}
.composer__coord {
  margin-top: 3px;
  font-size: var(--fs-9);
  line-height: 14px;
  color: var(--ink-3);
  cursor: help;
}
.composer__coord--experimental {
  color: var(--warn);
}
.composer__input {
  min-height: 30px;
  padding: 4px 8px;
  font-size: 12px;
}
.composer__select { min-width: 110px; }
.composer__input--num { width: 88px; }
/* Custom pickers match the field height and fill their column. */
.composer__opt :deep(.app-select),
.composer__opt :deep(.harness-select),
.composer__opt :deep(.dep-picker) { width: 100%; }
.composer__opt :deep(.app-select__trigger),
.composer__opt :deep(.dep-picker-trigger) {
  box-sizing: border-box;
  min-height: 30px;
  width: 100%;
}
.composer__tags {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
  background: var(--bg-sunk);
  border: 1px solid var(--rule);
  border-radius: var(--r-md);
  padding: 3px 6px;
  box-sizing: border-box;
  min-height: 30px;
}
.composer__tag-chip {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  background: var(--tint-neutral);
  border: 1px solid var(--rule-2);
  border-radius: var(--r-pill);
  padding: 1px 4px 1px 8px;
  font: 500 var(--fs-9) / 1.5 var(--font-mono);
  color: var(--ink-2);
}
.composer__tag-x { background: none; border: none; cursor: pointer; color: var(--ink-3); font-size: 13px; line-height: 1; padding: 0 2px; }
.composer__tag-x:hover { color: var(--ink); }
.composer__tag-input { flex: 1; min-width: 60px; background: none; border: none; outline: none; color: var(--ink); font-size: 12px; }
.composer__tag-input::placeholder { color: var(--ink-4); }
/* Labels add 18px above each control; the label-less utility buttons take the
   same offset so they sit on the control row. */
.composer__utils {
  display: flex;
  align-items: center;
  gap: 4px;
  margin-top: 18px;
  align-self: flex-start;
}
.composer__more {
  min-height: 30px;
}
.composer__actions {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  padding-top: 8px;
  border-top: 1px solid var(--rule);
}
.composer__toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: var(--fs-10);
  color: var(--ink-3);
  cursor: pointer;
  white-space: nowrap;
  flex-shrink: 0;
}
.composer__toggle input { margin: 0; accent-color: var(--accent); }
.composer__toggle-hint { color: var(--ink-4); font-family: var(--font-mono); font-size: var(--fs-9); }
.composer__spacer { flex: 1 1 auto; }
/* Cancel + Save move as a unit so a wrap never strands one on another line. */
.composer__btn-group { display: inline-flex; align-items: center; gap: 6px; flex-shrink: 0; }
</style>
