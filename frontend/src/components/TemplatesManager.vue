<script setup lang="ts">
import { ref, computed, watch } from 'vue';
import { api } from '../api/client';
import { renderMarkdown } from '../lib/markdown';
import type { PromptTemplate } from '../api/types';

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  changed: [];
}>();

const templates = ref<PromptTemplate[]>([]);
const loading = ref(false);
const loadError = ref('');
const newName = ref('');
const newBody = ref('');
const addStatus = ref('');
const saving = ref(false);
const bodyTab = ref<'edit' | 'preview'>('edit');

const bodyPreviewHtml = computed(() => renderMarkdown(newBody.value || ''));

watch(
  () => props.modelValue,
  async (open) => {
    if (!open) return;
    newName.value = '';
    newBody.value = '';
    addStatus.value = '';
    bodyTab.value = 'edit';
    await refresh();
  },
);

async function refresh() {
  loading.value = true;
  loadError.value = '';
  try {
    templates.value = await api<PromptTemplate[]>('GET', '/api/templates');
  } catch (e) {
    loadError.value = e instanceof Error ? e.message : 'Error loading templates.';
    templates.value = [];
  } finally {
    loading.value = false;
  }
}

async function saveNewTemplate() {
  const name = newName.value.trim();
  const body = newBody.value.trim();
  if (!name || !body) {
    addStatus.value = 'Name and body are required.';
    return;
  }
  saving.value = true;
  addStatus.value = 'Saving…';
  try {
    await api('POST', '/api/templates', { name, body });
    newName.value = '';
    newBody.value = '';
    bodyTab.value = 'edit';
    addStatus.value = 'Saved.';
    setTimeout(() => {
      if (addStatus.value === 'Saved.') addStatus.value = '';
    }, 2000);
    await refresh();
    emit('changed');
  } catch (e) {
    addStatus.value = 'Error: ' + (e instanceof Error ? e.message : 'unknown');
  } finally {
    saving.value = false;
  }
}

async function deleteTemplate(id: string) {
  if (!window.confirm('Delete this template?')) return;
  try {
    await api('DELETE', `/api/templates/${id}`);
    await refresh();
    emit('changed');
  } catch (e) {
    window.alert('Error deleting template: ' + (e instanceof Error ? e.message : 'unknown'));
  }
}

function close() {
  emit('update:modelValue', false);
}

function onOverlayClick(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('modal-overlay')) close();
}
</script>

<template>
  <div
    v-if="modelValue"
    class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4"
    @click="onOverlayClick"
  >
    <div
      class="modal-card"
      style="max-width:600px;width:100%;max-height:90vh;display:flex;flex-direction:column;"
    >
      <div class="p-6" style="display:flex;flex-direction:column;flex:1;min-height:0;">
        <div
          style="display:flex;align-items:center;justify-content:space-between;margin-bottom:16px;"
        >
          <h3 style="font-size:16px;font-weight:600;margin:0;">Prompt Templates</h3>
          <button
            type="button"
            style="background:none;border:none;cursor:pointer;font-size:20px;color:var(--text-muted);line-height:1;"
            aria-label="Close"
            @click="close"
          >
            &times;
          </button>
        </div>

        <!-- Add form -->
        <div
          id="tmpl-add-form"
          style="border:1px solid var(--border);border-radius:8px;padding:12px;margin-bottom:16px;"
        >
          <div
            style="font-size:11px;font-weight:600;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;margin-bottom:8px;"
          >
            Add Template
          </div>
          <input
            v-model="newName"
            type="text"
            placeholder="Name&hellip;"
            class="field"
            style="font-size:12px;padding:5px 8px;margin-bottom:6px;width:100%;box-sizing:border-box;"
          />
          <div
            style="display:flex;align-items:center;justify-content:space-between;margin-bottom:4px;"
          >
            <span class="text-xs text-v-muted">Body</span>
            <div class="logs-tabs">
              <button
                type="button"
                class="logs-tab"
                :class="{ active: bodyTab === 'edit' }"
                @click="bodyTab = 'edit'"
              >
                Edit
              </button>
              <button
                type="button"
                class="logs-tab"
                :class="{ active: bodyTab === 'preview' }"
                @click="bodyTab = 'preview'"
              >
                Preview
              </button>
            </div>
          </div>
          <textarea
            v-show="bodyTab === 'edit'"
            v-model="newBody"
            rows="4"
            placeholder="Prompt body&hellip;"
            class="field editable-field"
            style="font-size:12px;padding:5px 8px;width:100%;box-sizing:border-box;resize:vertical;"
          ></textarea>
          <div
            v-show="bodyTab === 'preview'"
            class="code-block prose-content editable-preview"
            style="font-size:12px;"
            v-html="bodyPreviewHtml"
          ></div>
          <div style="display:flex;align-items:center;gap:8px;margin-top:8px;">
            <button
              type="button"
              class="btn btn-accent"
              style="font-size:12px;"
              :disabled="saving"
              @click="saveNewTemplate"
            >
              Save
            </button>
            <span style="font-size:11px;color:var(--text-muted);">{{ addStatus }}</span>
          </div>
        </div>

        <!-- Existing templates list -->
        <div id="tmpl-list" style="overflow-y:auto;flex:1;min-height:0;">
          <div
            v-if="loading"
            style="font-size:12px;color:var(--text-muted);padding:8px 0;"
          >
            Loading&hellip;
          </div>
          <div
            v-else-if="loadError"
            style="font-size:12px;color:var(--text-muted);padding:8px 0;"
          >
            Error loading templates.
          </div>
          <div
            v-else-if="templates.length === 0"
            style="font-size:12px;color:var(--text-muted);padding:8px 0;"
          >
            No templates yet. Add one above.
          </div>
          <div
            v-for="t in templates"
            v-else
            :key="t.id"
            style="display:flex;align-items:flex-start;gap:10px;padding:10px 0;border-bottom:1px solid var(--border);"
          >
            <div style="flex:1;min-width:0;">
              <div style="font-size:13px;font-weight:500;color:var(--text-primary);">
                {{ t.name }}
              </div>
              <div
                style="font-size:11px;color:var(--text-muted);margin-top:3px;white-space:pre-wrap;word-break:break-word;max-height:48px;overflow:hidden;"
              >
                {{ t.body }}
              </div>
            </div>
            <button
              type="button"
              class="btn-icon"
              style="font-size:11px;padding:3px 8px;flex-shrink:0;color:var(--text-muted);"
              @click="deleteTemplate(t.id)"
            >
              Delete
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
