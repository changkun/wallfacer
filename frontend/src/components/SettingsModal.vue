<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue';
import SettingsTabAppearance from './settings/SettingsTabAppearance.vue';
import SettingsTabExecution from './settings/SettingsTabExecution.vue';
import SettingsTabSandbox from './settings/SettingsTabSandbox.vue';
import SettingsTabWorkspace from './settings/SettingsTabWorkspace.vue';
import SettingsTabPrompts from './settings/SettingsTabPrompts.vue';
import SettingsTabTrash from './settings/SettingsTabTrash.vue';
import SettingsTabAbout from './settings/SettingsTabAbout.vue';

const emit = defineEmits<{ close: []; workspaces: [] }>();

type TabKey = 'appearance' | 'execution' | 'sandbox' | 'workspace' | 'prompts' | 'trash' | 'about';
const activeTab = ref<TabKey>('appearance');

const tabs: { key: TabKey; label: string }[] = [
  { key: 'appearance', label: 'Appearance' },
  { key: 'execution', label: 'Execution' },
  { key: 'sandbox', label: 'Sandbox' },
  { key: 'workspace', label: 'Workspace' },
  { key: 'prompts', label: 'Prompts' },
  { key: 'trash', label: 'Trash' },
  { key: 'about', label: 'About' },
];

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close');
}
function onOverlayClick(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('modal-overlay')) emit('close');
}

onMounted(() => document.addEventListener('keydown', onKey));
onUnmounted(() => document.removeEventListener('keydown', onKey));
</script>

<template>
  <div class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4" @click="onOverlayClick">
    <div class="modal-card settings-modal-card" style="max-width: 840px; width: 100%">
      <div class="p-6 settings-modal-content">
        <div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px">
          <h3 style="font-size: 16px; font-weight: 600; margin: 0">Settings</h3>
          <button
            type="button"
            @click="emit('close')"
            style="background: none; border: none; cursor: pointer; font-size: 20px; color: var(--text-muted); line-height: 1"
            aria-label="Close settings"
          >&times;</button>
        </div>

        <div class="settings-layout">
          <div class="settings-tab-list" role="tablist" aria-label="Settings tabs">
            <button
              v-for="tab in tabs"
              :key="tab.key"
              type="button"
              class="settings-tab"
              :class="{ active: activeTab === tab.key }"
              role="tab"
              :aria-selected="activeTab === tab.key"
              @click="activeTab = tab.key"
            >{{ tab.label }}</button>
          </div>

          <div class="settings-tab-content-wrap">
            <SettingsTabAppearance v-if="activeTab === 'appearance'" />
            <SettingsTabExecution v-else-if="activeTab === 'execution'" />
            <SettingsTabSandbox v-else-if="activeTab === 'sandbox'" />
            <SettingsTabWorkspace v-else-if="activeTab === 'workspace'" @workspaces="emit('workspaces')" />
            <SettingsTabPrompts v-else-if="activeTab === 'prompts'" />
            <SettingsTabTrash v-else-if="activeTab === 'trash'" />
            <SettingsTabAbout v-else-if="activeTab === 'about'" />
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
