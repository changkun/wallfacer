<script setup lang="ts">
import { storeToRefs } from 'pinia';
import { SiteFooter as PlatformFooter } from 'latere-ui';
import 'latere-ui/styles';
import { usePrefsStore, type Locale } from '../stores/prefs';

const prefs = usePrefsStore();
const { theme, locale } = storeToRefs(prefs);

const localeOptions = [
  { code: 'en', label: 'EN', name: 'English' },
  { code: 'zh', label: '中', name: '中文' },
];

function onLocale(code: string) {
  if (code === 'en' || code === 'zh') prefs.setLocale(code as Locale);
}
</script>

<template>
  <!-- Shared Latere platform footer. Internal links resolve to https://latere.ai
       (Wallfacer's landing is on wf.latere.ai); theme/locale wired to the prefs
       store. Package styles fill the gaps wallfacer's app.css footer CSS lacks
       (4-col layout, subgroups, inline logo mark, full brand set, dropdown). -->
  <PlatformFooter
    v-model:theme="theme"
    :locale="locale"
    :locales="localeOptions"
    @update:locale="onLocale" />
</template>
