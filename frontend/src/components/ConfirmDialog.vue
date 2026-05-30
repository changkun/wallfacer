<script setup lang="ts">
import { useDialogStore } from '../stores/dialog';
const dialog = useDialogStore();

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
      class="confirm-overlay"
      tabindex="-1"
      @click.self="dialog.dismiss()"
      @keydown="onKeydown"
    >
      <div class="confirm-card" role="dialog" aria-modal="true">
        <h3 v-if="dialog.active.title" class="confirm-title">{{ dialog.active.title }}</h3>
        <p class="confirm-message">{{ dialog.active.message }}</p>
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
  </Teleport>
</template>

<style scoped>
.confirm-overlay {
  position: fixed;
  inset: 0;
  z-index: 100;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.4);
  padding: 16px;
}
.confirm-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 10px;
  padding: 20px;
  max-width: 420px;
  width: 100%;
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.25);
}
.confirm-title { margin: 0 0 8px; font-size: 14px; font-weight: 600; color: var(--text); }
.confirm-message { margin: 0 0 16px; font-size: 13px; color: var(--text); line-height: 1.5; white-space: pre-wrap; }
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
