<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { api } from '../api/client';
import { renderMarkdown } from '../lib/markdown';

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [boolean] }>();

type EditTab = 'edit' | 'preview';

const content = ref('');
const path = ref('');
const status = ref('');
const statusError = ref(false);
const activeTab = ref<EditTab>('preview');

const previewHtml = computed(() => renderMarkdown(content.value || ''));

let statusTimer: ReturnType<typeof setTimeout> | null = null;

function setStatus(msg: string, isError = false, autoClear = false) {
  if (statusTimer) {
    clearTimeout(statusTimer);
    statusTimer = null;
  }
  status.value = msg;
  statusError.value = isError;
  if (autoClear) {
    statusTimer = setTimeout(() => {
      status.value = '';
      statusError.value = false;
    }, 2000);
  }
}

function close() {
  emit('update:modelValue', false);
}

function onOverlayClick(e: MouseEvent) {
  if (e.target === e.currentTarget) close();
}

function switchTab(mode: EditTab) {
  activeTab.value = mode;
}

async function loadAll() {
  content.value = '';
  path.value = '';
  activeTab.value = 'preview';
  setStatus('Loading…');
  try {
    const config = await api<{ instructions_path?: string }>('GET', '/api/config');
    if (config.instructions_path) path.value = config.instructions_path;
  } catch {
    /* non-critical */
  }
  try {
    const data = await api<{ content?: string }>('GET', '/api/instructions');
    content.value = data.content || '';
    setStatus('');
    activeTab.value = 'preview';
  } catch (e) {
    setStatus(`Error loading: ${(e as Error).message}`, true);
  }
}

async function save() {
  setStatus('Saving…');
  try {
    await api('PUT', '/api/instructions', { content: content.value });
    setStatus('Saved.', false, true);
  } catch (e) {
    setStatus(`Error: ${(e as Error).message}`, true);
  }
}

async function reinit() {
  const ok = window.confirm(
    "Re-initialize from the default template and each repository's AGENTS.md (or legacy CLAUDE.md)? This will overwrite your current edits.",
  );
  if (!ok) return;
  setStatus('Re-initializing…');
  try {
    const data = await api<{ content?: string }>('POST', '/api/instructions/reinit');
    content.value = data.content || '';
    setStatus('Re-initialized.', false, true);
  } catch (e) {
    setStatus(`Error: ${(e as Error).message}`, true);
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      loadAll();
    } else {
      content.value = '';
      path.value = '';
      activeTab.value = 'preview';
      if (statusTimer) {
        clearTimeout(statusTimer);
        statusTimer = null;
      }
      status.value = '';
      statusError.value = false;
    }
  },
);
</script>

<template>
  <Teleport to="body">
    <div
      v-if="modelValue"
      class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4"
      @click="onOverlayClick"
    >
      <div
        class="modal-card"
        style="max-width: 720px; width: 100%; max-height: 90vh; display: flex; flex-direction: column;"
      >
        <div
          class="p-6"
          style="display: flex; flex-direction: column; flex: 1; min-height: 0;"
        >
          <div
            style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px;"
          >
            <h3 style="font-size: 16px; font-weight: 600; margin: 0;">
              Workspace AGENTS.md
            </h3>
            <button
              type="button"
              style="background: none; border: none; cursor: pointer; font-size: 20px; color: var(--text-muted); line-height: 1;"
              aria-label="Close"
              @click="close"
            >
              &times;
            </button>
          </div>

          <div
            style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px;"
          >
            <div
              style="font-size: 11px; color: var(--text-muted); font-family: monospace; word-break: break-all;"
            >
              {{ path }}
            </div>
            <div class="logs-tabs">
              <button
                type="button"
                class="logs-tab"
                :class="{ active: activeTab === 'edit' }"
                @click="switchTab('edit')"
              >
                Edit
              </button>
              <button
                type="button"
                class="logs-tab"
                :class="{ active: activeTab === 'preview' }"
                @click="switchTab('preview')"
              >
                Preview
              </button>
            </div>
          </div>

          <textarea
            v-show="activeTab === 'edit'"
            v-model="content"
            rows="22"
            spellcheck="false"
            class="field"
            style='font-family: "SF Mono", "Fira Code", "Consolas", monospace; font-size: 12px; flex: 1; min-height: 0; resize: none;'
          />
          <div
            v-show="activeTab === 'preview'"
            class="code-block prose-content editable-preview"
            style="flex: 1; min-height: 0;"
            v-html="previewHtml"
          />

          <div
            style="display: flex; align-items: center; gap: 8px; margin-top: 12px;"
          >
            <button type="button" class="btn btn-accent" @click="save">Save</button>
            <button type="button" class="btn-ghost" @click="close">Cancel</button>
            <button
              type="button"
              class="btn-icon"
              style="margin-left: 8px; font-size: 12px; padding: 4px 10px;"
              @click="reinit"
            >
              Re-init from repos
            </button>
            <span
              :style="{ fontSize: '12px', color: statusError ? '#d46868' : 'var(--text-muted)', marginLeft: 'auto' }"
            >{{ status }}</span>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>
