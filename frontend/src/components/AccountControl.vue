<script setup lang="ts">
// Connector between the shared latere-ui AccountMenu and wallfacer's session +
// prefs stores: feeds the principal in, wires switch-org / logout / login /
// navigate back to the store and router, tracks the org being switched for the
// per-row spinner, and hosts theme + language via the shared AccountPrefs in
// the menu's #prefs slot. Same pattern as lectio/lux so the chrome matches.
import { ref } from 'vue';
import { useRouter } from 'vue-router';
import { storeToRefs } from 'pinia';
import { AccountMenu, AccountPrefs, type LocaleOption, type AccountMenuItem } from 'latere-ui';

import { useAuthStore } from '../stores/auth';
import { usePrefsStore, type Locale } from '../stores/prefs';

withDefaults(
  defineProps<{
    placement?: 'top-end' | 'bottom-start';
    /** App-specific rows (e.g. Me / Admin) rendered inside the menu. */
    extraItems?: AccountMenuItem[];
  }>(),
  { placement: 'bottom-start', extraItems: () => [] },
);

const auth = useAuthStore();
const prefs = usePrefsStore();
const router = useRouter();
const { theme, locale } = storeToRefs(prefs);
const switching = ref<string | null>(null);

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
    :principal="auth.me"
    :placement="placement"
    :extra-items="extraItems"
    :labels="{ signIn: 'Sign in via latere.ai' }"
    :switching-org-id="switching"
    @switch-org="onSwitch"
    @logout="auth.logout()"
    @login="auth.login()"
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
</template>
