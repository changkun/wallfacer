<script setup lang="ts">
// Presentational modal for the local-mode device-code sign-in. State and the
// start/poll/cancel flow live in useDeviceSignIn (owned by AccountControl); this
// renders the user code, the verification link, and the terminal states, and
// emits cancel / retry. Styling mirrors ConfirmDialog.vue.
import { computed } from 'vue';
import { useT } from '../i18n';
import type { DeviceSignInStatus } from '../composables/useDeviceSignIn';

const props = defineProps<{
  status: DeviceSignInStatus;
  userCode: string;
  verificationUri: string;
  verificationUriComplete: string;
  error: string;
}>();

const emit = defineEmits<{ (e: 'cancel'): void; (e: 'retry'): void }>();

const t = useT();

const errorMessage = computed(() => {
  switch (props.error) {
    case 'denied':
      return t.value('auth.device.error.denied');
    case 'expired':
      return t.value('auth.device.error.expired');
    default:
      return t.value('auth.device.error.generic');
  }
});
</script>

<template>
  <Teleport to="body">
    <div
      class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4"
      style="z-index: 60;"
      tabindex="-1"
      @click.self="emit('cancel')"
      @keydown.esc="emit('cancel')"
    >
      <div class="modal-card" style="max-width: 420px; width: 100%;" role="dialog" aria-modal="true">
        <div class="p-6">
          <h3 class="device-title">{{ t('auth.device.title') }}</h3>

          <template v-if="status === 'pending' || status === 'starting'">
            <p class="device-step">{{ t('auth.device.step') }}</p>
            <div class="device-code" aria-label="verification code">{{ userCode }}</div>
            <a
              class="device-open"
              :href="verificationUriComplete || verificationUri"
              target="_blank"
              rel="noopener"
            >{{ t('auth.device.open') }}</a>
            <p class="device-waiting">{{ t('auth.device.waiting') }}</p>
            <div class="device-actions">
              <button type="button" class="device-btn device-btn--ghost" @click="emit('cancel')">
                {{ t('auth.device.cancel') }}
              </button>
            </div>
          </template>

          <template v-else-if="status === 'done'">
            <p class="device-success">{{ t('auth.device.success') }}</p>
          </template>

          <template v-else-if="status === 'error'">
            <p class="device-error">{{ errorMessage }}</p>
            <div class="device-actions">
              <button type="button" class="device-btn device-btn--ghost" @click="emit('cancel')">
                {{ t('auth.device.close') }}
              </button>
              <button type="button" class="device-btn device-btn--primary" @click="emit('retry')">
                {{ t('auth.device.retry') }}
              </button>
            </div>
          </template>
        </div>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
.device-title { margin: 0 0 12px; font-size: 14px; font-weight: 600; color: var(--text); }
.device-step { margin: 0 0 10px; font-size: 13px; color: var(--text); line-height: 1.5; }
.device-code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 26px;
  font-weight: 700;
  letter-spacing: 3px;
  text-align: center;
  padding: 12px;
  margin: 0 0 12px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-input, var(--bg-card));
  color: var(--text);
  user-select: all;
}
.device-open {
  display: inline-block;
  margin: 0 0 12px;
  font-size: 13px;
  color: var(--accent);
  text-decoration: none;
}
.device-open:hover { text-decoration: underline; }
.device-waiting { margin: 0; font-size: 12px; color: var(--text-muted, #888); }
.device-success { margin: 0; font-size: 13px; color: var(--text); }
.device-error { margin: 0 0 16px; font-size: 13px; color: var(--err, #c0392b); line-height: 1.5; }
.device-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 16px; }
.device-btn {
  padding: 6px 14px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg-card);
  color: var(--text);
  font-size: 13px;
  cursor: pointer;
}
.device-btn--ghost:hover { background: var(--bg-hover); }
.device-btn--primary { background: var(--accent); border-color: var(--accent); color: #fff; }
.device-btn--primary:hover { opacity: 0.9; }
</style>
