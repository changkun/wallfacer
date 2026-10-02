<script setup lang="ts">
// Connector between the shared latere-ui AccountMenu and wallfacer's session +
// prefs stores: feeds the principal in, wires switch-org / logout / login /
// navigate back to the store and router, tracks the org being switched for the
// per-row spinner, and hosts theme + language via the shared AccountPrefs in
// the menu's #prefs slot. Same pattern as the other latere.ai products so the
// chrome matches.
import { computed, ref } from 'vue';
import { useRouter } from 'vue-router';
import { storeToRefs } from 'pinia';
import { AccountMenu, AccountPrefs, type LocaleOption, type AccountMenuItem, type Principal } from 'latere-ui';

import { useAuthStore } from '../stores/auth';
import { accountRole } from '../lib/accountRole';
import { usePrefsStore, type Locale } from '../stores/prefs';
import { useSignIn } from '../composables/useSignIn';
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
// roleless (the dropdown shows the org name, not a fabricated tier); the
// platform_admin role and the no-org individual are unambiguous.
const principal = computed<Principal | null>(() => {
  const m = auth.me;
  if (!m) return null;
  const role = accountRole(m);
  return { ...m, role };
});
const prefs = usePrefsStore();
const router = useRouter();
const { theme, locale } = storeToRefs(prefs);
const switching = ref<string | null>(null);

// Sign-in is the shared flow in useSignIn: device code first, the browser
// redirect to /login as the fallback, and the reason in the modal where the
// server reports that redirect off.
const signIn = useSignIn();
const { modalOpen: signInModalOpen, modalProps: signInModalProps } = signIn;

const localeOptions: LocaleOption[] = [
  { code: 'en', label: 'EN', name: 'English' },
  { code: 'zh', label: '中', name: '中文' },
];

function onSwitch(id: string) {
  // An org switch completes through /login, so it is not started where that
  // redirect is off; the session and its org stay as they are.
  if (!signIn.redirectEnabled.value) {
    signIn.failRedirectUnavailable();
    return;
  }
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
    @login="signIn.start()"
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
    v-if="signInModalOpen"
    v-bind="signInModalProps"
    @cancel="signIn.cancel()"
    @retry="signIn.start()"
  />
</template>
