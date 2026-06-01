<script setup lang="ts">
import { nextTick, ref, computed, onMounted, watch } from 'vue';
import { useTaskStore } from '../stores/tasks';
import { api } from '../api/client';
import { splitBatch, flowAllowsEmptyPrompt } from '../lib/composer';
import { useMentions } from '../composables/useMentions';
import DependencyPicker from './DependencyPicker.vue';
import TemplatePicker from './TemplatePicker.vue';
import { getStored, setStored, removeStored } from '../lib/storage';
import type { PromptTemplate } from '../api/types';

interface FlowOption { slug: string; name: string; spawn_kind?: string }

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
const templates = ref<PromptTemplate[]>([]);
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

const timeoutMin = ref<number | null>(null);

// Flow-aware placeholder hint, mirroring the legacy data-task-flow behavior.
const allowsEmptyPrompt = computed(() => flowAllowsEmptyPrompt(flow.value, flows.value));
const promptPlaceholder = computed(() => {
  const f = flow.value || 'implement';
  if (allowsEmptyPrompt.value) {
    return `Optional: focus the ${f} (leave blank to let the agent pick from workspace signals)`;
  }
  return `Describe the task… (flow: ${f} · Markdown, @ to mention files, ${modKey}↵ to save)`;
});
// Optional overrides (behind a "More" toggle).
const showMore = ref(false);
const model = ref('');
const maxCostUsd = ref<number | null>(null);
const maxInputTokens = ref<number | null>(null);
const dependsOn = ref<string[]>([]);
const sandbox = ref<'' | 'claude' | 'codex'>('');
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

async function loadTemplates() {
  if (templates.value.length) return;
  try {
    templates.value = await api<PromptTemplate[]>('GET', '/api/templates');
  } catch (e) {
    console.error('load templates:', e);
  }
}

function insertTemplate(body: string) {
  if (!body) return;
  const el = textareaRef.value;
  if (el) {
    const pos = el.selectionStart ?? el.value.length;
    const before = el.value.slice(0, pos);
    const after = el.value.slice(pos);
    // Insert with a leading newline if the cursor isn't at the start of a
    // blank line — keeps the inserted block from glueing onto the prior text.
    const sep = before && !before.endsWith('\n') ? '\n' : '';
    prompt.value = before + sep + body + after;
    nextTick(() => {
      const newPos = (before + sep + body).length;
      el.focus();
      el.setSelectionRange(newPos, newPos);
    });
  } else {
    prompt.value = prompt.value ? prompt.value + '\n' + body : body;
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
  loadTemplates();
  await nextTick();
  textareaRef.value?.focus();
}

function collapse() {
  expanded.value = false;
  prompt.value = '';
  tags.value = []; tagDraft.value = '';
  timeoutMin.value = null;
  showMore.value = false;
  model.value = '';
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
  // Brainstorm / idea-agent flows accept an empty prompt (the agent builds its
  // own from workspace signals); every other flow requires one.
  if ((!text && !allowsEmptyPrompt.value) || submitting.value) return;
  submitting.value = true;
  try {
    if (scheduled.value) {
      await submitRoutine(text);
      collapse();
      return;
    }
    const sharedOpts = {
      flow: flow.value || undefined,
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
        // sandbox overrides is a follow-up PATCH (see CLAUDE.md task lifecycle).
        if (sandbox.value) patch.sandbox = sandbox.value;
        if (Object.keys(patch).length) {
          await store.patchTask(created.id, patch);
        }
      }
    }

    prompt.value = '';
    tags.value = []; tagDraft.value = '';
    timeoutMin.value = null;
    model.value = '';
    maxCostUsd.value = null;
    maxInputTokens.value = null;
    dependsOn.value = [];
    sandbox.value = '';
    batchMode.value = false;
    expanded.value = false;
  } catch (e) {
    console.error('create task:', e);
  } finally {
    submitting.value = false;
  }
}

function clear() {
  prompt.value = '';
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
    class="composer-add"
    @click="expand"
  >
    + New Task
  </button>
  <form v-else class="composer" @submit.prevent="submit">
    <div class="composer__prompt-wrap">
      <textarea
        ref="textareaRef"
        v-model="prompt"
        class="composer__prompt"
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
        <span class="composer__opt-label">Flow</span>
        <select v-model="flow" class="composer__select" aria-label="Flow">
          <option v-for="f in flows" :key="f.slug" :value="f.slug">{{ f.name }}</option>
        </select>
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
        <input
          v-model.number="timeoutMin"
          class="composer__input composer__input--num"
          type="number"
          min="1"
          placeholder="min"
          aria-label="Timeout in minutes"
        />
      </label>
      <label v-if="templates.length" class="composer__opt">
        <span class="composer__opt-label">Template</span>
        <TemplatePicker :templates="templates" @select="insertTemplate" />
      </label>
      <button
        type="button"
        class="composer__more"
        title="Mention a file (@)"
        aria-label="Insert @ mention"
        @click="insertAtMention"
      >@</button>
      <button type="button" class="composer__more" @click="showMore = !showMore">
        {{ showMore ? '− Less' : '+ More' }}
      </button>
    </div>
    <div v-if="showMore" class="composer__opts">
      <label class="composer__opt composer__opt--grow">
        <span class="composer__opt-label">Model</span>
        <input v-model="model" class="composer__input" type="text" placeholder="override model" aria-label="Model override" />
      </label>
      <label class="composer__opt">
        <span class="composer__opt-label">Max $</span>
        <input v-model.number="maxCostUsd" class="composer__input composer__input--num" type="number" min="0" step="0.5" placeholder="USD" aria-label="Max cost USD" />
      </label>
      <label class="composer__opt">
        <span class="composer__opt-label">Max tokens</span>
        <input v-model.number="maxInputTokens" class="composer__input composer__input--num" type="number" min="0" step="1000" placeholder="input" aria-label="Max input tokens" />
      </label>
      <div v-if="depCandidates.length" class="composer__opt composer__opt--grow">
        <span class="composer__opt-label">Depends on</span>
        <DependencyPicker v-model="dependsOn" />
      </div>
      <label class="composer__opt">
        <span class="composer__opt-label">Sandbox</span>
        <select v-model="sandbox" class="composer__select" aria-label="Sandbox override">
          <option value="">Default (agent)</option>
          <option value="claude">Claude</option>
          <option value="codex">Codex</option>
        </select>
      </label>
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
          class="composer__input composer__input--num"
          type="number"
          min="1"
          placeholder="min"
          aria-label="Interval minutes"
        />
      </label>
      <span class="composer__spacer" />
      <button
        type="button"
        class="composer__btn composer__btn--ghost"
        @click="collapse"
      >
        Cancel
      </button>
      <button
        type="button"
        class="composer__btn composer__btn--ghost"
        :disabled="!prompt.trim() || submitting"
        @click="clear"
      >
        Clear
      </button>
      <button
        type="submit"
        class="composer__btn composer__btn--primary"
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
  </form>
</template>

<style scoped>
.composer__prompt-wrap { position: relative; }
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
  border: 1px solid var(--border);
  border-radius: 6px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.18);
}
.composer__mention {
  padding: 4px 8px;
  font-size: 12px;
  font-family: var(--font-mono);
  border-radius: 4px;
  cursor: pointer;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.composer__mention.active,
.composer__mention:hover { background: var(--bg-hover); }
.composer__opts {
  display: flex;
  gap: 8px;
  margin-top: 6px;
  align-items: flex-end;
  flex-wrap: wrap;
}
.composer__opt {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.composer__opt--grow { flex: 1 1 auto; min-width: 120px; }
.composer__opt-label {
  font-size: 10px;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.03em;
}
.composer__select,
.composer__input {
  background: var(--bg-input);
  border: 1px solid var(--border);
  color: var(--text);
  border-radius: 6px;
  padding: 4px 8px;
  font-size: 12px;
  font-family: var(--font-sans);
  outline: none;
}
.composer__input--num { width: 72px; }
.composer__tags {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 4px;
  background: var(--bg-input);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 3px 6px;
  min-height: 28px;
}
.composer__tag-chip {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  background: var(--bg-hover);
  border-radius: 4px;
  padding: 1px 4px 1px 6px;
  font-size: 11px;
}
.composer__tag-x { background: none; border: none; cursor: pointer; color: var(--text-muted); font-size: 13px; line-height: 1; padding: 0 2px; }
.composer__tag-x:hover { color: var(--text); }
.composer__tag-input { flex: 1; min-width: 60px; background: none; border: none; outline: none; color: var(--text); font-size: 12px; }
.composer__select:focus,
.composer__input:focus { border-color: var(--accent); }
.composer__more {
  background: none;
  border: none;
  color: var(--text-muted);
  font-size: 11px;
  cursor: pointer;
  padding: 4px 6px;
}
.composer__more:hover { color: var(--text); }
.composer__toggle {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--text-muted);
  cursor: pointer;
}
.composer__toggle input { margin: 0; accent-color: var(--accent); }
.composer__toggle-hint { color: var(--text-muted); font-family: var(--font-mono); font-size: 10px; }
.composer__spacer { flex: 1 1 auto; }
</style>
