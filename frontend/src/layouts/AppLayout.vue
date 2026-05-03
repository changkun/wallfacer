<script setup lang="ts">
import { ref } from 'vue';
import Sidebar from '../components/Sidebar.vue';
import StatusBar from '../components/StatusBar.vue';
import SettingsModal from '../components/SettingsModal.vue';
import { useSse } from '../composables/useSse';
import { useTaskStore } from '../stores/tasks';
import { useKeyboard } from '../composables/useKeyboard';
import type { Task } from '../api/types';

const store = useTaskStore();
const sidebarCollapsed = ref(false);
const showSettings = ref(false);

const { connected } = useSse({
  url: '/api/tasks/stream',
  listeners: {
    snapshot: (data) => store.setTasks(data as Task[]),
    'task-updated': (data) => store.updateTask(data as Task),
    'task-deleted': (data) => store.removeTask((data as { id: string }).id),
  },
});

useKeyboard({
  onSearch: () => document.querySelector<HTMLInputElement>('.search-input')?.focus(),
  onNewTask: () => document.querySelector<HTMLTextAreaElement>('.composer-input')?.focus(),
  onSettings: () => { showSettings.value = !showSettings.value; },
});
</script>

<template>
  <div class="app-shell">
    <Sidebar :collapsed="sidebarCollapsed" @toggle="sidebarCollapsed = !sidebarCollapsed" @settings="showSettings = true" />
    <div class="app-main">
      <slot :connected="connected" />
      <StatusBar :connected="connected" />
    </div>
    <SettingsModal v-if="showSettings" @close="showSettings = false" />
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
