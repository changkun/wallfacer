<script setup lang="ts">
// Connector between the shared latere-ui AccountMenu and wallfacer's session +
// prefs stores: feeds the principal in, wires switch-org / logout / login /
// navigate back to the store and router, tracks the org being switched for the
// per-row spinner, and hosts theme + language via the shared AccountPrefs in
// the menu's #prefs slot. Same pattern as lectio/lux so the chrome matches.
import { computed, ref, watch } from 'vue';
import { useRouter } from 'vue-router';
import { storeToRefs } from 'pinia';
import { AccountMenu, AccountPrefs, type LocaleOption, type AccountMenuItem, type Principal } from 'latere-ui';

import { useAuthStore } from '../stores/auth';
import { usePrefsStore, type Locale } from '../stores/prefs';
import { useDeviceSignIn } from '../composables/useDeviceSignIn';
import DeviceSignInModal from './DeviceSignInModal.vue';

withDefaults(
  defineProps<{
    placement?: 'top-end' | 'bottom-start';
    /** App-specific rows (e.g. Me / Admin) rendered inside the menu. */
    extraItems?: AccountMenuItem[];
  }>(),
  { placement: 'bottom-start', extraItems: () => [] },
);

const auth = useAuthStore();

// Derive the account role so the shared AccountMenu renders the role badge +
// dropdown descriptor (as in lux). null in anonymous local-run mode.
// wallfacer's /api/me carries no org-admin signal, so org users are left
// roleless (the dropdown shows the org name, not a fabricated tier);
// superadmin and no-org individual are unambiguous.
const principal = computed<Principal | null>(() => {
  const m = auth.me;
  if (!m) return null;
  const role = m.is_superadmin ? 'platform_admin' : m.org_id ? undefined : 'individual';
  return { ...m, role };
});
const prefs = usePrefsStore();
const router = useRouter();
const { theme, locale } = storeToRefs(prefs);
const switching = ref<string | null>(null);

// Local-mode sign-in uses the RFC 8628 device-code flow: onLogin starts it and,
// when device sign-in is unavailable (cloud deployments answer 503), falls back
// to the shared store's browser /login redirect. A completed flow refreshes the
// principal so the menu re-renders as signed-in, then clears the modal.
const device = useDeviceSignIn();
const {
  status: deviceStatus,
  userCode: deviceUserCode,
  verificationUri: deviceVerificationUri,
  verificationUriComplete: deviceVerificationUriComplete,
  error: deviceError,
} = device;

function onLogin() {
  void device.loginOrFallback(auth.login);
}

watch(device.status, async (s) => {
  if (s === 'done') {
    await auth.fetchMe();
    window.setTimeout(() => device.reset(), 1200);
  }
});

const localeOptions: LocaleOption[] = [
  { code: 'en', label: 'EN', name: 'English' },
  { code: 'zh', label: '中', name: '中文' },
];

function onSwitch(id: string) {
  switching.value = id;
  void auth.switchOrg(id);
}
function onSetLocale(code: string) {
  prefs.setLocale(code as Locale);
}
</script>

<template>
  <AccountMenu
    :principal="principal"
    :placement="placement"
    :extra-items="extraItems"
    :labels="{ signIn: 'Sign in via latere.ai' }"
    :switching-org-id="switching"
    @switch-org="onSwitch"
    @logout="auth.logout()"
    @login="onLogin()"
    @navigate="(p: string) => router.push(p)"
  >
    <template #prefs>
      <AccountPrefs
        :theme="theme"
        :locale="locale"
        :locale-options="localeOptions"
        @set-theme="prefs.setTheme"
        @set-locale="onSetLocale"
      />
    </template>
  </AccountMenu>

  <DeviceSignInModal
    v-if="deviceStatus !== 'idle' && deviceStatus !== 'starting'"
    :status="deviceStatus"
    :user-code="deviceUserCode"
    :verification-uri="deviceVerificationUri"
    :verification-uri-complete="deviceVerificationUriComplete"
    :error="deviceError"
    @cancel="device.cancel()"
    @retry="onLogin()"
  />
</template>
