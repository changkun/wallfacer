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
        class="dialog confirm"
        :class="{ 'confirm--danger': dialog.active.danger }"
        role="dialog"
        aria-modal="true"
      >
        <div v-if="dialog.active.title" class="dialog-head">
          <div class="dialog-head__main">
            <h3 class="dialog-title">{{ dialog.active.title }}</h3>
          </div>
        </div>
        <div class="dialog-body confirm-body" :class="{ 'confirm-body--untitled': !dialog.active.title }">
          <svg
            v-if="dialog.active.danger"
            class="confirm-icon"
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <circle cx="12" cy="12" r="10"></circle>
            <line x1="12" y1="8" x2="12" y2="12"></line>
            <line x1="12" y1="16" x2="12.01" y2="16"></line>
          </svg>
          <div class="confirm-main">
            <p class="confirm-message">{{ dialog.active.message }}</p>
            <input
              v-if="dialog.active.prompt"
              ref="promptInput"
              type="text"
              class="field confirm-input"
              :value="promptText"
              :placeholder="dialog.active.prompt.placeholder || ''"
              @input="onPromptInput"
            />
          </div>
        </div>
        <div class="dialog-foot confirm-actions">
          <button
            v-if="!dialog.active.alert"
            type="button"
            class="btn ghost confirm-btn confirm-btn--ghost"
            @click="dialog.dismiss()"
          >{{ dialog.active.cancelLabel }}</button>
          <button
            type="button"
            class="btn confirm-btn"
            :class="dialog.active.danger ? 'ghost danger confirm-btn--danger' : 'confirm-btn--primary'"
            @click="dialog.accept()"
          >{{ dialog.active.confirmLabel }}</button>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
/* The confirm is the 440px dialog: message (with a warning glyph when the
   action is destructive), an optional prompt field, and a foot whose forward
   button is ink, or the danger ghost when it destroys something. */
.confirm-body {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}
.confirm-body--untitled {
  padding-top: 20px;
}
.confirm-icon {
  flex: none;
  margin-top: 1px;
  color: var(--err);
}
.confirm-main {
  flex: 1;
  min-width: 0;
}
.confirm-message {
  margin: 0;
  font-size: var(--fs-md);
  line-height: 1.5;
  color: var(--ink);
  white-space: pre-wrap;
}
.confirm-input {
  margin-top: 12px;
}
</style>
