<script setup lang="ts">
import { nextTick, ref, watch, computed } from 'vue';
import { useDialogStore } from '../stores/dialog';
import { useFocusTrap } from '../composables/useFocusTrap';
const dialog = useDialogStore();
const promptInput = ref<HTMLInputElement | null>(null);
const promptText = ref('');
const cardRef = ref<HTMLElement | null>(null);
useFocusTrap(cardRef, computed(() => !!dialog.active));

watch(() => dialog.active, async (a) => {
  if (a?.prompt) {
    promptText.value = a.prompt.initial ?? '';
    dialog.setPromptValue(promptText.value);
    await nextTick();
    promptInput.value?.focus();
    promptInput.value?.select();
  }
}, { immediate: true });

function onPromptInput(e: Event) {
  promptText.value = (e.target as HTMLInputElement).value;
  dialog.setPromptValue(promptText.value);
}

function onKeydown(e: KeyboardEvent) {
  if (!dialog.active) return;
  if (e.key === 'Escape') { e.preventDefault(); dialog.dismiss(); }
  if (e.key === 'Enter') { e.preventDefault(); dialog.accept(); }
}
</script>

<template>
  <Teleport to="body">
    <div
      v-if="dialog.active"
      class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4"
      style="z-index: 60;"
      tabindex="-1"
      @click.self="dialog.dismiss()"
      @keydown="onKeydown"
    >
      <div
        ref="cardRef"
        class="modal-card"
        style="max-width: 420px; width: 100%;"
        role="dialog"
        aria-modal="true"
      >
        <div class="p-6">
          <h3 v-if="dialog.active.title" class="confirm-title">{{ dialog.active.title }}</h3>
          <div class="confirm-body">
            <svg
              v-if="dialog.active.danger"
              class="confirm-icon"
              width="20"
              height="20"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              stroke-linecap="round"
              stroke-linejoin="round"
            >
              <circle cx="12" cy="12" r="10"></circle>
              <line x1="12" y1="8" x2="12" y2="12"></line>
              <line x1="12" y1="16" x2="12.01" y2="16"></line>
            </svg>
            <p class="confirm-message">{{ dialog.active.message }}</p>
          </div>
          <input
            v-if="dialog.active.prompt"
            ref="promptInput"
            type="text"
            class="confirm-input"
            :value="promptText"
            :placeholder="dialog.active.prompt.placeholder || ''"
            @input="onPromptInput"
          />
          <div class="confirm-actions">
          <button
            v-if="!dialog.active.alert"
            type="button"
            class="confirm-btn confirm-btn--ghost"
            @click="dialog.dismiss()"
          >{{ dialog.active.cancelLabel }}</button>
          <button
            type="button"
            class="confirm-btn"
            :class="dialog.active.danger ? 'confirm-btn--danger' : 'confirm-btn--primary'"
            @click="dialog.accept()"
          >{{ dialog.active.confirmLabel }}</button>
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.confirm-title { margin: 0 0 8px; font-size: 14px; font-weight: 600; color: var(--text); }
.confirm-body { display: flex; align-items: flex-start; gap: 12px; margin: 0 0 16px; }
.confirm-icon { color: #e05252; flex-shrink: 0; margin-top: 1px; }
.confirm-message { margin: 0; font-size: 13px; color: var(--text); line-height: 1.5; white-space: pre-wrap; }
.confirm-input {
  display: block;
  width: 100%;
  padding: 6px 10px;
  margin: 0 0 16px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-input, var(--bg-card));
  color: var(--text);
  font-size: 13px;
  box-sizing: border-box;
}
.confirm-input:focus { outline: 2px solid var(--accent); outline-offset: -1px; }
.confirm-actions { display: flex; justify-content: flex-end; gap: 8px; }
.confirm-btn {
  padding: 6px 14px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text);
  font-size: 13px;
  cursor: pointer;
}
.confirm-btn--ghost:hover { background: var(--bg-hover); }
.confirm-btn--primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.confirm-btn--danger { background: var(--err, #c0392b); border-color: var(--err, #c0392b); color: #fff; }
.confirm-btn--primary:hover, .confirm-btn--danger:hover { opacity: 0.9; }
</style>
