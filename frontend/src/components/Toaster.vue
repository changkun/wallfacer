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
        class="pop toast"
        :class="'toast--' + t.kind"
        role="status"
      >
        <span class="toast__dot" aria-hidden="true"></span>
        <span class="toast__msg">{{ t.message }}</span>
        <button
          v-if="t.action"
          type="button"
          class="btn sm toast__action"
          @click="t.action.run()"
        >{{ t.action.label }}</button>
        <button type="button" class="icon-btn sm toast__close" aria-label="Dismiss" @click="toast.dismiss(t.id)">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><line x1="6" y1="6" x2="18" y2="18"></line><line x1="18" y1="6" x2="6" y2="18"></line></svg>
        </button>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
/* A bottom-right stack of popover rows: a tone dot, the message, an optional
   ink action and a quiet dismiss. The tone lives in the dot, not a stripe. */
.toaster {
  position: fixed;
  right: 16px;
  bottom: 16px;
  z-index: 120;
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: min(380px, calc(100vw - 32px));
}
.toast {
  flex-direction: row;
  align-items: center;
  gap: 10px;
  padding: 10px 8px 10px 14px;
  font-size: var(--fs-base);
  color: var(--ink);
}
.toast__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex: none;
  background: var(--run);
}
.toast--success .toast__dot { background: var(--ok); }
.toast--error .toast__dot { background: var(--err); }
.toast__msg {
  flex: 1 1 auto;
  min-width: 0;
  line-height: 1.4;
}
.toast__action {
  white-space: nowrap;
}
</style>
