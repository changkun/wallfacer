<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import SettingsTabExecution from '../components/settings/SettingsTabExecution.vue';
import SettingsTabSandbox from '../components/settings/SettingsTabSandbox.vue';
import SettingsTabWorkspace from '../components/settings/SettingsTabWorkspace.vue';
import SettingsTabPrompts from '../components/settings/SettingsTabPrompts.vue';
import SettingsTabGithub from '../components/settings/SettingsTabGithub.vue';
import SettingsTabAbout from '../components/settings/SettingsTabAbout.vue';
import { useUiStore } from '../stores/ui';

type TabKey = 'execution' | 'sandbox' | 'workspace' | 'prompts' | 'github' | 'about';

const route = useRoute();
const router = useRouter();
const ui = useUiStore();

const tabs: { key: TabKey; label: string; icon: string }[] = [
  { key: 'execution', label: 'Execution', icon: 'M13 2L3 14h9l-1 8 10-12h-9z' },
  { key: 'sandbox', label: 'Harness', icon: 'M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z' },
  { key: 'workspace', label: 'Workspace', icon: 'M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z' },
  { key: 'prompts', label: 'Prompts', icon: 'M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8zM14 2v6h6M16 13H8M16 17H8M10 9H8' },
  { key: 'github', label: 'GitHub', icon: 'M9 19c-5 1.5-5-2.5-7-3m14 6v-3.87a3.37 3.37 0 0 0-.94-2.61c3.14-.35 6.44-1.54 6.44-7A5.44 5.44 0 0 0 20 4.77 5.07 5.07 0 0 0 19.91 1S18.73.65 16 2.48a13.38 13.38 0 0 0-7 0C6.27.65 5.09 1 5.09 1A5.07 5.07 0 0 0 5 4.77a5.44 5.44 0 0 0-1.5 3.78c0 5.42 3.3 6.61 6.44 7A3.37 3.37 0 0 0 9 18.13V22' },
  { key: 'about', label: 'About', icon: 'M12 22a10 10 0 1 1 0-20 10 10 0 0 1 0 20zM12 16v-4M12 8h.01' },
];

const activeTab = computed<TabKey>(() => {
  const t = (route.query.tab as TabKey) || 'execution';
  return tabs.some(x => x.key === t) ? t : 'execution';
});

function selectTab(key: TabKey) {
  router.replace({ path: route.path, query: { ...route.query, tab: key } });
}

function openWorkspacePicker() {
  ui.openWorkspaces();
}

onMounted(() => {
  if (!route.query.tab) selectTab('execution');
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
          <SettingsTabExecution v-if="activeTab === 'execution'" />
          <SettingsTabSandbox v-else-if="activeTab === 'sandbox'" />
          <SettingsTabWorkspace v-else-if="activeTab === 'workspace'" @workspaces="openWorkspacePicker" />
          <SettingsTabPrompts v-else-if="activeTab === 'prompts'" />
          <SettingsTabGithub v-else-if="activeTab === 'github'" />
          <SettingsTabAbout v-else-if="activeTab === 'about'" />
        </div>
      </div>
    </div>
  </div>
</template>
