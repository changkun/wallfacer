<script setup lang="ts">
import { ref, computed, watch } from 'vue';
import { api } from '../api/client';
import { renderMarkdown } from '../lib/markdown';
import type { SystemPromptTemplate } from '../api/types';

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [boolean] }>();

type EditTab = 'edit' | 'preview';

const templates = ref<SystemPromptTemplate[]>([]);
const currentName = ref<string>('');
const editorContent = ref<string>('');
const promptsDir = ref<string>('');
const loadError = ref<string>('');
const status = ref<string>('');
const statusIsError = ref<boolean>(false);
const activeTab = ref<EditTab>('preview');
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
  return !tmpl || !tmpl.has_override;
});

const previewHtml = computed<string>(() => renderMarkdown(editorContent.value || ''));

function close() {
  emit('update:modelValue', false);
}

function onOverlayClick(e: MouseEvent) {
  if (e.target === e.currentTarget) close();
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

function switchTab(mode: EditTab) {
  activeTab.value = mode;
}

function selectTemplate(name: string) {
  currentName.value = name;
  const tmpl = templates.value.find((t) => t.name === name);
  editorContent.value = tmpl ? tmpl.content : '';
  // Match old behavior: switch to preview when selecting a template.
  activeTab.value = 'preview';
  setStatus('', false, false);
}

function decorateName(name: string): string {
  // Insert zero-width spaces in underscores so long names wrap nicely.
  return name.replace(/_/g, '​_');
}

async function loadConfig() {
  try {
    const cfg = await api<{ prompts_dir?: string }>('GET', '/api/config');
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
    // Refresh content/override flag for the still-selected template.
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
  setStatus('Saving…', false, false);
  try {
    await api('PUT', `/api/system-prompts/${encodeURIComponent(currentName.value)}`, {
      content: editorContent.value,
    });
    setStatus('Saved.');
    await loadTemplates();
  } catch (e) {
    setStatus(`Error: ${e instanceof Error ? e.message : String(e)}`, true, false);
  }
}

async function resetToDefault() {
  const tmpl = currentTemplate.value;
  if (!tmpl || !tmpl.has_override) return;
  const ok = window.confirm(
    `Reset "${tmpl.name}" to the embedded default? Your override will be deleted.`,
  );
  if (!ok) return;
  setStatus('Resetting…', false, false);
  try {
    await api('DELETE', `/api/system-prompts/${encodeURIComponent(tmpl.name)}`);
    setStatus('Reset to default.');
    await loadTemplates();
  } catch (e) {
    setStatus(`Error: ${e instanceof Error ? e.message : String(e)}`, true, false);
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
      activeTab.value = 'preview';
    }
  },
  { immediate: true },
);
</script>

<template>
  <Teleport to="body">
    <div
      v-if="modelValue"
      id="system-prompts-modal"
      class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4"
      @click="onOverlayClick"
    >
      <div
        class="modal-card"
        style="
          max-width: 860px;
          width: 100%;
          max-height: 92vh;
          display: flex;
          flex-direction: column;
        "
      >
        <div
          class="p-6"
          style="display: flex; flex-direction: column; flex: 1; min-height: 0"
        >
          <!-- Header -->
          <div
            style="
              display: flex;
              align-items: center;
              justify-content: space-between;
              margin-bottom: 4px;
            "
          >
            <h3 style="font-size: 16px; font-weight: 600; margin: 0">
              System Prompts
            </h3>
            <button
              type="button"
              aria-label="Close system prompts"
              style="
                background: none;
                border: none;
                cursor: pointer;
                font-size: 20px;
                color: var(--text-muted);
                line-height: 1;
              "
              @click="close"
            >
              &times;
            </button>
          </div>
          <div
            style="
              font-size: 11px;
              color: var(--text-muted);
              margin-bottom: 12px;
              font-family: monospace;
              word-break: break-all;
            "
          >
            {{ promptsDir }}
          </div>

          <!-- Two-column layout: list on left, editor on right -->
          <div
            style="
              display: flex;
              gap: 12px;
              flex: 1;
              min-height: 0;
              overflow: hidden;
            "
          >
            <!-- Template list -->
            <div
              style="
                width: 190px;
                flex-shrink: 0;
                overflow-y: auto;
                display: flex;
                flex-direction: column;
                gap: 2px;
              "
            >
              <div
                v-if="loadError"
                style="font-size: 11px; color: var(--color-error, #e53e3e); padding: 6px"
              >
                Error loading templates: {{ loadError }}
              </div>
              <button
                v-for="tmpl in templates"
                :key="tmpl.name"
                type="button"
                :data-name="tmpl.name"
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
                  :title="tmpl.has_override ? 'User override active' : 'Using embedded default'"
                  :style="{
                    width: '6px',
                    height: '6px',
                    borderRadius: '50%',
                    flexShrink: 0,
                    background: tmpl.has_override ? 'var(--accent, #d97757)' : 'transparent',
                    border: '1px solid ' + (tmpl.has_override ? 'var(--accent, #d97757)' : 'var(--border, #ccc)'),
                  }"
                ></span>
                <span style="overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ decorateName(tmpl.name) }}</span>
              </button>
            </div>

            <!-- Editor pane -->
            <div
              style="flex: 1; display: flex; flex-direction: column; min-height: 0"
            >
              <div
                style="
                  display: flex;
                  flex-direction: column;
                  flex: 1;
                  min-height: 0;
                "
              >
                <div
                  style="
                    display: flex;
                    align-items: center;
                    justify-content: space-between;
                    margin-bottom: 6px;
                  "
                >
                  <div
                    style="
                      font-size: 12px;
                      font-weight: 600;
                      color: var(--text-secondary);
                    "
                  >
                    {{ headerLabel }}
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
                  v-model="editorContent"
                  rows="22"
                  class="field"
                  spellcheck="false"
                  style='font-family: "SF Mono", "Fira Code", "Consolas", monospace; font-size: 12px; flex: 1; min-height: 0; resize: none;'
                  placeholder="Select a template on the left to edit it."
                ></textarea>
                <div
                  v-show="activeTab === 'preview'"
                  class="code-block prose-content editable-preview"
                  style="flex: 1; min-height: 0"
                  v-html="previewHtml"
                ></div>
              </div>

              <!-- Actions -->
              <div
                style="
                  display: flex;
                  align-items: center;
                  gap: 8px;
                  margin-top: 12px;
                "
              >
                <button
                  type="button"
                  class="btn btn-accent"
                  :disabled="!currentName"
                  @click="saveOverride"
                >
                  Save override
                </button>
                <button
                  type="button"
                  class="btn-icon"
                  :disabled="resetDisabled"
                  :style="{ fontSize: '12px', padding: '4px 10px', opacity: resetDisabled ? 0.4 : 1 }"
                  @click="resetToDefault"
                >
                  Reset to default
                </button>
                <button type="button" class="btn-ghost" @click="close">Close</button>
                <span
                  :style="{
                    fontSize: '12px',
                    color: statusIsError ? 'var(--color-error, #e53e3e)' : 'var(--text-muted)',
                    marginLeft: 'auto',
                    minHeight: '1em',
                  }"
                >{{ status }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>
