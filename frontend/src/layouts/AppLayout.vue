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
import { useKeyboard } from '../composables/useKeyboard';
import type { Task } from '../api/types';

const store = useTaskStore();
const sidebarCollapsed = ref(false);
const showSettings = ref(false);
const showPalette = ref(false);
const showWorkspaces = ref(false);
const showContainers = ref(false);

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
  onSearch: () => { showPalette.value = true; },
  onNewTask: () => document.querySelector<HTMLTextAreaElement>('.composer-input')?.focus(),
  onSettings: () => { showSettings.value = !showSettings.value; },
});
</script>

<template>
  <div class="app-shell">
    <Sidebar
      :collapsed="sidebarCollapsed"
      @toggle="sidebarCollapsed = !sidebarCollapsed"
      @settings="showSettings = true"
      @palette="showPalette = true"
      @workspaces="showWorkspaces = true"
      @containers="showContainers = true"
    />
    <div class="app-main">
      <slot :connected="connected" />
      <StatusBar :connected="connected" />
    </div>
    <SettingsModal v-if="showSettings" @close="showSettings = false" />
    <CommandPalette v-model="showPalette" />
    <WorkspacePicker v-model="showWorkspaces" />
    <ContainerMonitor v-model="showContainers" />
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
