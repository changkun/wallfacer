<script setup lang="ts">
import { ref, computed, watch } from 'vue';
import { api } from '../api/client';
import type { SystemPromptTemplate, ServerConfig } from '../api/types';

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [boolean] }>();

const templates = ref<SystemPromptTemplate[]>([]);
const currentName = ref<string>('');
const editorContent = ref<string>('');
const promptsDir = ref<string>('');
const loadError = ref<string>('');
const status = ref<string>('');
const statusIsError = ref<boolean>(false);
const saving = ref(false);
const resetting = ref(false);
let statusTimer: ReturnType<typeof setTimeout> | null = null;

const currentTemplate = computed<SystemPromptTemplate | undefined>(() =>
  templates.value.find((t) => t.name === currentName.value),
);

const headerLabel = computed<string>(() => {
  const tmpl = currentTemplate.value;
  if (!tmpl) return '';
  return `${tmpl.name} (${tmpl.has_override ? 'override active' : 'embedded default'})`;
});

const resetDisabled = computed<boolean>(() => {
  const tmpl = currentTemplate.value;
  return !tmpl || !tmpl.has_override || resetting.value;
});

function close() {
  emit('update:modelValue', false);
}

function onOverlayClick(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('modal-overlay')) close();
}

function setStatus(text: string, isError = false, autoClear = true) {
  if (statusTimer) {
    clearTimeout(statusTimer);
    statusTimer = null;
  }
  status.value = text;
  statusIsError.value = isError;
  if (autoClear && text) {
    statusTimer = setTimeout(() => {
      status.value = '';
      statusTimer = null;
    }, 2000);
  }
}

function selectTemplate(name: string) {
  currentName.value = name;
  const tmpl = templates.value.find((t) => t.name === name);
  editorContent.value = tmpl ? tmpl.content : '';
  setStatus('', false, false);
}

function decorateName(name: string): string {
  // Insert zero-width spaces in underscores so long names wrap nicely.
  return name.replace(/_/g, '​_');
}

async function loadConfig() {
  try {
    const cfg = await api<ServerConfig>('GET', '/api/config');
    promptsDir.value = cfg.prompts_dir || '';
  } catch {
    promptsDir.value = '';
  }
}

async function loadTemplates() {
  loadError.value = '';
  try {
    const data = await api<SystemPromptTemplate[]>('GET', '/api/system-prompts');
    templates.value = Array.isArray(data) ? data : [];
  } catch (e) {
    templates.value = [];
    loadError.value = e instanceof Error ? e.message : 'Failed to load templates';
    return;
  }

  if (
    currentName.value &&
    templates.value.some((t) => t.name === currentName.value)
  ) {
    // Refresh content/override flag for the still-selected template without
    // resetting unsaved edits to a different one.
    const tmpl = templates.value.find((t) => t.name === currentName.value);
    if (tmpl) editorContent.value = tmpl.content;
  } else if (templates.value.length > 0) {
    selectTemplate(templates.value[0].name);
  } else {
    currentName.value = '';
    editorContent.value = '';
  }
}

async function saveOverride() {
  if (!currentName.value) return;
  saving.value = true;
  setStatus('Saving...', false, false);
  try {
    await api('PUT', `/api/system-prompts/${encodeURIComponent(currentName.value)}`, {
      content: editorContent.value,
    });
    setStatus('Saved.');
    await loadTemplates();
  } catch (e) {
    setStatus(`Error: ${e instanceof Error ? e.message : String(e)}`, true, false);
  } finally {
    saving.value = false;
  }
}

async function resetToDefault() {
  const tmpl = currentTemplate.value;
  if (!tmpl || !tmpl.has_override) return;
  const ok = window.confirm(
    `Reset "${tmpl.name}" to the embedded default? Your override will be deleted.`,
  );
  if (!ok) return;
  resetting.value = true;
  setStatus('Resetting...', false, false);
  try {
    await api('DELETE', `/api/system-prompts/${encodeURIComponent(tmpl.name)}`);
    setStatus('Reset to default.');
    await loadTemplates();
  } catch (e) {
    setStatus(`Error: ${e instanceof Error ? e.message : String(e)}`, true, false);
  } finally {
    resetting.value = false;
  }
}

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      void loadConfig();
      void loadTemplates();
    } else {
      if (statusTimer) {
        clearTimeout(statusTimer);
        statusTimer = null;
      }
      status.value = '';
      statusIsError.value = false;
      loadError.value = '';
    }
  },
  { immediate: true },
);
</script>

<template>
  <div
    v-if="modelValue"
    class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4"
    @click="onOverlayClick"
  >
    <div
      class="modal-card"
      style="max-width: 1000px; width: 100%; max-height: 90vh; display: flex; flex-direction: column"
    >
      <div class="p-6" style="display: flex; flex-direction: column; flex: 1; min-height: 0">
        <div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; gap: 12px">
          <h3 style="font-size: 16px; font-weight: 600; margin: 0">System Prompts</h3>
          <button
            type="button"
            @click="close"
            style="background: none; border: none; cursor: pointer; font-size: 20px; color: var(--text-muted); line-height: 1"
            aria-label="Close system prompts"
          >&times;</button>
        </div>

        <div
          v-if="promptsDir"
          style="margin-bottom: 12px; font-size: 11px; color: var(--text-muted); font-family: monospace; word-break: break-all"
          :title="promptsDir"
        >{{ promptsDir }}</div>

        <div style="display: flex; gap: 12px; flex: 1; min-height: 0">
          <div
            style="width: 200px; flex-shrink: 0; display: flex; flex-direction: column; gap: 4px; overflow-y: auto; border-right: 1px solid var(--border); padding-right: 10px"
          >
            <div
              v-if="loadError"
              style="font-size: 11px; color: var(--color-error, #e53e3e); padding: 6px"
            >{{ loadError }}</div>
            <div
              v-else-if="templates.length === 0"
              style="font-size: 11px; color: var(--text-muted); padding: 6px"
            >No templates available.</div>
            <button
              v-for="tmpl in templates"
              :key="tmpl.name"
              type="button"
              :data-name="tmpl.name"
              :title="tmpl.has_override ? 'User override active' : 'Using embedded default'"
              :style="{
                display: 'flex',
                alignItems: 'center',
                gap: '6px',
                width: '100%',
                textAlign: 'left',
                padding: '5px 8px',
                border: '1px solid ' + (currentName === tmpl.name ? 'var(--border)' : 'transparent'),
                borderRadius: '5px',
                background: currentName === tmpl.name ? 'var(--bg-active, rgba(128,128,128,0.15))' : 'none',
                cursor: 'pointer',
                fontSize: '12px',
                color: 'var(--text-secondary)',
              }"
              @click="selectTemplate(tmpl.name)"
            >
              <span
                :style="{
                  width: '6px',
                  height: '6px',
                  borderRadius: '50%',
                  flexShrink: 0,
                  background: tmpl.has_override ? 'var(--accent, #d97757)' : 'transparent',
                  border: '1px solid ' + (tmpl.has_override ? 'var(--accent, #d97757)' : 'var(--border, #ccc)'),
                }"
              ></span>
              <span style="overflow: hidden; text-overflow: ellipsis; white-space: nowrap">
                {{ decorateName(tmpl.name) }}
              </span>
            </button>
          </div>

          <div style="flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 8px">
            <div
              style="display: flex; align-items: center; justify-content: space-between; gap: 8px; min-height: 22px"
            >
              <div style="font-size: 12px; font-weight: 600; color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap">
                {{ headerLabel }}
              </div>
            </div>
            <textarea
              v-model="editorContent"
              :disabled="!currentName"
              spellcheck="false"
              style="flex: 1; min-height: 320px; width: 100%; resize: none; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; line-height: 1.5; padding: 10px; border-radius: 6px; border: 1px solid var(--border); background: var(--bg-secondary, transparent); color: var(--text-primary); box-sizing: border-box"
            ></textarea>
            <div style="display: flex; align-items: center; justify-content: space-between; gap: 10px">
              <span
                :style="{
                  fontSize: '11px',
                  color: statusIsError ? 'var(--color-error, #e53e3e)' : 'var(--text-muted)',
                }"
              >{{ status }}</span>
              <div style="display: flex; align-items: center; gap: 8px">
                <button
                  type="button"
                  class="btn-icon"
                  :disabled="resetDisabled"
                  :style="{ fontSize: '12px', padding: '4px 12px', opacity: resetDisabled ? 0.4 : 1 }"
                  @click="resetToDefault"
                >Reset to default</button>
                <button
                  type="button"
                  class="btn-icon"
                  style="font-size: 12px; padding: 4px 12px"
                  :disabled="!currentName || saving"
                  @click="saveOverride"
                >{{ saving ? 'Saving...' : 'Save' }}</button>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
