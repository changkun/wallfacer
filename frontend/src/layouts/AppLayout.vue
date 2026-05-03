<script setup lang="ts">
import { ref, onMounted } from 'vue';
import Sidebar from '../components/Sidebar.vue';
import StatusBar from '../components/StatusBar.vue';
import SettingsModal from '../components/SettingsModal.vue';
import CommandPalette from '../components/CommandPalette.vue';
import WorkspacePicker from '../components/WorkspacePicker.vue';
import ContainerMonitor from '../components/ContainerMonitor.vue';
import { useSse } from '../composables/useSse';
import { useTaskStore } from '../stores/tasks';
import { useUiStore } from '../stores/ui';
import { useKeyboard } from '../composables/useKeyboard';
import type { Task } from '../api/types';

const store = useTaskStore();
const ui = useUiStore();
const sidebarCollapsed = ref(false);

onMounted(async () => {
  if (!store.config) await store.fetchConfig();
});

const { connected } = useSse({
  url: '/api/tasks/stream',
  listeners: {
    snapshot: (data) => store.setTasks(data as Task[]),
    'task-updated': (data) => store.updateTask(data as Task),
    'task-deleted': (data) => store.removeTask((data as { id: string }).id),
  },
});

useKeyboard({
  onSearch: () => { ui.showPalette = true; },
  onNewTask: () => document.querySelector<HTMLTextAreaElement>('.composer-input')?.focus(),
  onSettings: () => { ui.showSettings = !ui.showSettings; },
});
</script>

<template>
  <div class="app-shell">
    <Sidebar
      :collapsed="sidebarCollapsed"
      @toggle="sidebarCollapsed = !sidebarCollapsed"
      @settings="ui.showSettings = true"
      @palette="ui.showPalette = true"
      @workspaces="ui.showWorkspaces = true"
      @containers="ui.showContainers = true"
    />
    <div class="app-main">
      <slot :connected="connected" />
      <StatusBar :connected="connected" />
    </div>
    <SettingsModal
      v-if="ui.showSettings"
      @close="ui.showSettings = false"
      @workspaces="ui.showSettings = false; ui.showWorkspaces = true"
    />
    <CommandPalette v-model="ui.showPalette" />
    <WorkspacePicker v-model="ui.showWorkspaces" />
    <ContainerMonitor v-model="ui.showContainers" />
  </div>
</template>

<style scoped>
.app-shell {
  display: flex;
  height: 100vh;
  background: var(--bg);
  color: var(--ink);
  font-family: var(--font-sans);
  font-size: 13px;
}
.app-main {
  display: flex;
  flex-direction: column;
  flex: 1;
  overflow: hidden;
}
</style>
