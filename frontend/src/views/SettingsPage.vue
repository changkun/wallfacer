<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import SettingsTabAppearance from '../components/settings/SettingsTabAppearance.vue';
import SettingsTabExecution from '../components/settings/SettingsTabExecution.vue';
import SettingsTabSandbox from '../components/settings/SettingsTabSandbox.vue';
import SettingsTabWorkspace from '../components/settings/SettingsTabWorkspace.vue';
import SettingsTabPrompts from '../components/settings/SettingsTabPrompts.vue';
import SettingsTabTrash from '../components/settings/SettingsTabTrash.vue';
import SettingsTabAbout from '../components/settings/SettingsTabAbout.vue';
import { useUiStore } from '../stores/ui';

type TabKey = 'appearance' | 'execution' | 'sandbox' | 'workspace' | 'prompts' | 'trash' | 'about';

const route = useRoute();
const router = useRouter();
const ui = useUiStore();

const tabs: { key: TabKey; label: string; icon: string }[] = [
  { key: 'appearance', label: 'Appearance', icon: 'M12 3v2M12 19v2M5 12H3M21 12h-2M7 7l-1.5-1.5M18.5 18.5L17 17M7 17l-1.5 1.5M18.5 5.5L17 7M12 7a5 5 0 1 0 0 10 5 5 0 0 0 0-10z' },
  { key: 'execution', label: 'Execution', icon: 'M13 2L3 14h9l-1 8 10-12h-9z' },
  { key: 'sandbox', label: 'Sandbox', icon: 'M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z' },
  { key: 'workspace', label: 'Workspace', icon: 'M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z' },
  { key: 'prompts', label: 'Prompts', icon: 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8zM14 2v6h6M16 13H8M16 17H8M10 9H8' },
  { key: 'trash', label: 'Trash', icon: 'M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6' },
  { key: 'about', label: 'About', icon: 'M12 22a10 10 0 1 1 0-20 10 10 0 0 1 0 20zM12 16v-4M12 8h.01' },
];

const activeTab = computed<TabKey>(() => {
  const t = (route.query.tab as TabKey) || 'appearance';
  return tabs.some(x => x.key === t) ? t : 'appearance';
});

function selectTab(key: TabKey) {
  router.replace({ path: route.path, query: { ...route.query, tab: key } });
}

function openWorkspacePicker() {
  ui.openWorkspaces();
}

onMounted(() => {
  if (!route.query.tab) selectTab('appearance');
});
</script>

<template>
  <div class="settings-page">
    <div class="settings-page-inner">
      <div class="settings-page-head">
        <div class="settings-page-eyebrow">Settings</div>
        <h1 class="settings-page-title">Workspace settings</h1>
      </div>

      <div class="set-grid">
        <div class="set-side" role="tablist" aria-label="Settings tabs">
          <button
            v-for="t in tabs"
            :key="t.key"
            type="button"
            role="tab"
            class="set-tab"
            :class="{ 'is-active': activeTab === t.key }"
            :aria-selected="activeTab === t.key"
            @click="selectTab(t.key)"
          >
            <span class="set-tab-icon">
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
                <path :d="t.icon"></path>
              </svg>
            </span>
            <span>{{ t.label }}</span>
          </button>
        </div>

        <div class="set-body">
          <SettingsTabAppearance v-if="activeTab === 'appearance'" />
          <SettingsTabExecution v-else-if="activeTab === 'execution'" />
          <SettingsTabSandbox v-else-if="activeTab === 'sandbox'" />
          <SettingsTabWorkspace v-else-if="activeTab === 'workspace'" @workspaces="openWorkspacePicker" />
          <SettingsTabPrompts v-else-if="activeTab === 'prompts'" />
          <SettingsTabTrash v-else-if="activeTab === 'trash'" />
          <SettingsTabAbout v-else-if="activeTab === 'about'" />
        </div>
      </div>
    </div>
  </div>
</template>
