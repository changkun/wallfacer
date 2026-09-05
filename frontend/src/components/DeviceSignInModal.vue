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
      <div class="dialog device" role="dialog" aria-modal="true">
        <div class="dialog-head">
          <div class="dialog-head__main">
            <h3 class="dialog-title device-title">{{ t('auth.device.title') }}</h3>
          </div>
        </div>

        <template v-if="status === 'pending' || status === 'starting'">
          <div class="dialog-body">
            <p class="device-step">{{ t('auth.device.step') }}</p>
            <div class="device-code" aria-label="verification code">{{ userCode }}</div>
            <a
              class="link device-open"
              :href="verificationUriComplete || verificationUri"
              target="_blank"
              rel="noopener"
            >{{ t('auth.device.open') }}</a>
            <p class="device-waiting muted">{{ t('auth.device.waiting') }}</p>
          </div>
          <div class="dialog-foot device-actions">
            <button type="button" class="btn ghost device-btn device-btn--ghost" @click="emit('cancel')">
              {{ t('auth.device.cancel') }}
            </button>
          </div>
        </template>

        <template v-else-if="status === 'done'">
          <div class="dialog-body">
            <p class="device-success">{{ t('auth.device.success') }}</p>
          </div>
        </template>

        <template v-else-if="status === 'error'">
          <div class="dialog-body">
            <p class="device-error">{{ errorMessage }}</p>
          </div>
          <div class="dialog-foot device-actions">
            <button type="button" class="btn ghost device-btn device-btn--ghost" @click="emit('cancel')">
              {{ t('auth.device.close') }}
            </button>
            <button type="button" class="btn device-btn device-btn--primary" @click="emit('retry')">
              {{ t('auth.device.retry') }}
            </button>
          </div>
        </template>
      </div>
    </div>
  </Teleport>
</template>

<style scoped>
/* Device sign-in is the 440px dialog: the one-time code sits in a sunk mono
   block the user can select in one gesture. */
.device-step,
.device-success,
.device-error {
  margin: 0;
  font-size: var(--fs-md);
  line-height: 1.5;
  color: var(--ink);
}
.device-error {
  color: var(--err);
}
.device-code {
  margin: 12px 0;
  padding: 14px;
  font: 700 26px / 1 var(--font-mono);
  letter-spacing: 0.12em;
  text-align: center;
  color: var(--ink);
  background: var(--bg-sunk);
  border: 1px solid var(--rule);
  border-radius: var(--r-lg);
  user-select: all;
}
.device-open {
  display: inline-block;
  font-size: var(--fs-base);
}
.device-waiting {
  margin: 10px 0 0;
  font-size: var(--fs-10);
}
</style>
