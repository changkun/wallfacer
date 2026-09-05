<script setup lang="ts">
import { computed, onMounted, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import AnalyticsTabUsage from '../components/analytics/AnalyticsTabUsage.vue';
import AnalyticsTabCost from '../components/analytics/AnalyticsTabCost.vue';
import AnalyticsTabTiming from '../components/analytics/AnalyticsTabTiming.vue';

type TabKey = 'usage' | 'analytics' | 'timing';

const VALID_TABS: TabKey[] = ['usage', 'analytics', 'timing'];

const route = useRoute();
const router = useRouter();

function normalizeTab(raw: unknown): TabKey {
  const v = Array.isArray(raw) ? raw[0] : raw;
  if (typeof v === 'string' && (VALID_TABS as string[]).includes(v)) {
    return v as TabKey;
  }
  return 'usage';
}

const activeTab = computed<TabKey>(() => normalizeTab(route.query.tab));

function selectTab(tab: TabKey) {
  if (tab === activeTab.value) return;
  router.replace({ query: { ...route.query, tab } });
}

onMounted(() => {
  if (!route.query.tab) {
    router.replace({ query: { ...route.query, tab: 'usage' } });
  }
});

// Re-validate on direct URL edits.
watch(() => route.query.tab, (raw) => {
  const v = Array.isArray(raw) ? raw[0] : raw;
  if (typeof v !== 'string' || !(VALID_TABS as string[]).includes(v)) {
    router.replace({ query: { ...route.query, tab: 'usage' } });
  }
});
</script>

<template>
  <div class="analytics-page">
    <div class="analytics-page__inner">
      <div class="an-head">
        <div>
          <h1 class="an-title">Analytics</h1>
          <p class="an-lede">Usage, spend, and execution timing across the workspace.</p>
        </div>
        <div class="tabs an-tabs" role="tablist">
          <button
            type="button"
            class="tab"
            :class="{ on: activeTab === 'usage' }"
            role="tab"
            :aria-selected="activeTab === 'usage'"
            data-tab="usage"
            @click="selectTab('usage')"
          >Usage</button>
          <button
            type="button"
            class="tab"
            :class="{ on: activeTab === 'analytics' }"
            role="tab"
            :aria-selected="activeTab === 'analytics'"
            data-tab="analytics"
            @click="selectTab('analytics')"
          >Tokens &amp; cost</button>
          <button
            type="button"
            class="tab"
            :class="{ on: activeTab === 'timing' }"
            role="tab"
            :aria-selected="activeTab === 'timing'"
            data-tab="timing"
            @click="selectTab('timing')"
          >Execution timing</button>
        </div>
      </div>
      <AnalyticsTabUsage v-if="activeTab === 'usage'" />
      <AnalyticsTabCost v-else-if="activeTab === 'analytics'" />
      <AnalyticsTabTiming v-else-if="activeTab === 'timing'" />
    </div>
  </div>
</template>
