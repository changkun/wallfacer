<script setup lang="ts">
import { nextTick, ref, computed } from 'vue';
import { useTaskStore } from '../stores/tasks';
import { api } from '../api/client';
import { parseTags } from '../lib/composer';
import { useMentions } from '../composables/useMentions';

interface FlowOption { slug: string; name: string }

const store = useTaskStore();
const prompt = ref('');
const mentions = useMentions({ setValue: (v) => { prompt.value = v; }, priorityPrefix: 'spec/' });
const submitting = ref(false);
const expanded = ref(false);
const textareaRef = ref<HTMLTextAreaElement | null>(null);
const modKey = typeof navigator !== 'undefined' && /Mac/.test(navigator.platform) ? '⌘' : 'Ctrl';

// Advanced fields.
const flows = ref<FlowOption[]>([]);
const flow = ref('implement');
const tagsInput = ref('');
const timeoutMin = ref<number | null>(null);
// Optional overrides (behind a "More" toggle).
const showMore = ref(false);
const model = ref('');
const maxCostUsd = ref<number | null>(null);
const maxInputTokens = ref<number | null>(null);
const dependsOn = ref<string[]>([]);

// Candidate dependencies: existing non-archived tasks (most recent first).
const depCandidates = computed(() =>
  store.tasks
    .filter((t) => !t.archived && t.kind !== 'routine')
    .map((t) => ({ id: t.id, label: (t.title || t.prompt || t.id).slice(0, 60) })),
);

async function loadFlows() {
  if (flows.value.length) return;
  try {
    const res = await api<FlowOption[] | { flows: FlowOption[] }>('GET', '/api/flows');
    flows.value = Array.isArray(res) ? res : (res?.flows ?? []);
  } catch (e) {
    console.error('load flows:', e);
  }
}

async function expand() {
  expanded.value = true;
  loadFlows();
  await nextTick();
  textareaRef.value?.focus();
}

function collapse() {
  expanded.value = false;
  prompt.value = '';
  tagsInput.value = '';
  timeoutMin.value = null;
  showMore.value = false;
  model.value = '';
  maxCostUsd.value = null;
  maxInputTokens.value = null;
  dependsOn.value = [];
}

async function submit() {
  const text = prompt.value.trim();
  if (!text || submitting.value) return;
  submitting.value = true;
  try {
    const created = await store.createTask(text, {
      flow: flow.value || undefined,
      tags: parseTags(tagsInput.value),
      timeout: timeoutMin.value && timeoutMin.value > 0 ? timeoutMin.value : undefined,
      model: model.value.trim() || undefined,
      maxCostUsd: maxCostUsd.value ?? undefined,
      maxInputTokens: maxInputTokens.value ?? undefined,
    });
    if (created?.id && dependsOn.value.length) {
      await store.patchTask(created.id, { depends_on: [...dependsOn.value] });
    }
    prompt.value = '';
    tagsInput.value = '';
    timeoutMin.value = null;
    model.value = '';
    maxCostUsd.value = null;
    maxInputTokens.value = null;
    dependsOn.value = [];
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
        :placeholder="`Describe the task… (Markdown supported, @ to mention files, ${modKey}↵ to save)`"
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
        <input
          v-model="tagsInput"
          class="composer__input"
          type="text"
          placeholder="comma,separated"
          aria-label="Tags"
        />
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
      <label v-if="depCandidates.length" class="composer__opt composer__opt--grow">
        <span class="composer__opt-label">Depends on</span>
        <select v-model="dependsOn" class="composer__select" multiple size="3" aria-label="Dependencies">
          <option v-for="d in depCandidates" :key="d.id" :value="d.id">{{ d.label }}</option>
        </select>
      </label>
    </div>
    <div class="composer__actions">
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
        :disabled="!prompt.trim() || submitting"
      >
        {{ submitting ? 'Saving…' : 'Save' }}
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
</style>
