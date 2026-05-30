<script setup lang="ts">
import { useToastStore } from '../stores/toast';
const toast = useToastStore();
</script>

<template>
  <Teleport to="body">
    <div class="toaster" aria-live="polite">
      <div
        v-for="t in toast.toasts"
        :key="t.id"
        class="toast"
        :class="'toast--' + t.kind"
        role="status"
      >
        <span class="toast__msg">{{ t.message }}</span>
        <button
          v-if="t.action"
          type="button"
          class="toast__action"
          @click="t.action.run()"
        >{{ t.action.label }}</button>
        <button type="button" class="toast__close" aria-label="Dismiss" @click="toast.dismiss(t.id)">&times;</button>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.toaster {
  position: fixed;
  right: 16px;
  bottom: 16px;
  z-index: 120;
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-width: 380px;
}
.toast {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 8px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.2);
  font-size: 13px;
  color: var(--text);
  animation: toast-in 160ms ease-out;
}
@keyframes toast-in {
  from { opacity: 0; transform: translateY(8px); }
  to { opacity: 1; transform: translateY(0); }
}
.toast--success { border-left: 3px solid var(--ok); }
.toast--error { border-left: 3px solid var(--err, #c0392b); }
.toast--info { border-left: 3px solid var(--accent); }
.toast__msg { flex: 1 1 auto; }
.toast__action {
  background: var(--accent);
  border: none;
  color: #fff;
  border-radius: 5px;
  padding: 3px 10px;
  font-size: 12px;
  cursor: pointer;
  white-space: nowrap;
}
.toast__close {
  background: none;
  border: none;
  color: var(--text-muted);
  font-size: 16px;
  line-height: 1;
  cursor: pointer;
}
.toast__close:hover { color: var(--text); }
</style>
