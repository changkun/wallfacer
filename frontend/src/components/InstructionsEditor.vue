<script setup lang="ts">
import { ref, watch } from 'vue';
import { api } from '../api/client';

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [boolean] }>();

const content = ref('');
const path = ref('');
const status = ref('');
const statusError = ref(false);

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

async function loadAll() {
  content.value = '';
  path.value = '';
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
        :style="{ maxWidth: '800px', width: '100%', maxHeight: '90vh', display: 'flex', flexDirection: 'column' }"
      >
        <div class="p-6" :style="{ display: 'flex', flexDirection: 'column', flex: '1', minHeight: '0' }">
          <div :style="{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '12px' }">
            <h3 :style="{ fontSize: '16px', fontWeight: 600, margin: '0' }">AGENTS.md</h3>
            <button
              type="button"
              :style="{ background: 'none', border: 'none', cursor: 'pointer', fontSize: '20px', color: 'var(--text-muted)', lineHeight: '1' }"
              aria-label="Close"
              @click="close"
            >
              &times;
            </button>
          </div>

          <div :style="{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '12px', marginBottom: '8px', fontSize: '12px' }">
            <code
              :style="{ color: 'var(--text-muted)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', flex: '1', minWidth: '0' }"
            >{{ path }}</code>
            <span :style="{ color: statusError ? '#d46868' : 'var(--text-muted)', whiteSpace: 'nowrap' }">{{ status }}</span>
          </div>

          <textarea
            v-model="content"
            rows="20"
            spellcheck="false"
            :style="{ flex: '1', minHeight: '0', width: '100%', fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace', fontSize: '12px', lineHeight: '1.5', resize: 'none' }"
          />

          <div :style="{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', gap: '8px', marginTop: '12px' }">
            <button type="button" class="btn-icon" @click="reinit">Re-init</button>
            <button type="button" class="btn-icon" @click="close">Cancel</button>
            <button type="button" class="btn btn-accent" @click="save">Save</button>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>
