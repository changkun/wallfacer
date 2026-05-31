<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import Sidebar from '../components/Sidebar.vue';
import StatusBar from '../components/StatusBar.vue';
import CommandPalette from '../components/CommandPalette.vue';
import WorkspacePicker from '../components/WorkspacePicker.vue';
import ContainerMonitor from '../components/ContainerMonitor.vue';
import InstructionsEditor from '../components/InstructionsEditor.vue';
import SystemPromptsManager from '../components/SystemPromptsManager.vue';
import TemplatesManager from '../components/TemplatesManager.vue';
import TerminalPanel from '../components/TerminalPanel.vue';
import KeyboardShortcutsModal from '../components/KeyboardShortcutsModal.vue';
import ConfirmDialog from '../components/ConfirmDialog.vue';
import Toaster from '../components/Toaster.vue';
import { useSse } from '../composables/useSse';
import { useTaskStore } from '../stores/tasks';
import { useUiStore } from '../stores/ui';
import { useKeyboard } from '../composables/useKeyboard';
import type { Task } from '../api/types';

const store = useTaskStore();
const ui = useUiStore();
const router = useRouter();
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
  // Server emits heartbeats every 15 s. If nothing arrives for 35 s the
  // connection has likely died silently — the watchdog inside useSse
  // restarts the stream and we refetch the canonical task list so any
  // missed delta gets repaired.
  onStaleRestart: () => { void store.fetchTasks({ includeArchived: ui.showArchived }); },
});

useKeyboard({
  onSearch: () => { ui.showPalette = true; },
  onFocusSearch: () => document.querySelector<HTMLInputElement>('.task-search-input')?.focus(),
  onNewTask: () => document.querySelector<HTMLTextAreaElement>('.composer-input')?.focus(),
  onSettings: () => { void router.push('/settings'); },
  onTerminal: () => { ui.toggleTerminal(); },
  onShortcuts: () => { ui.openShortcuts(); },
});
</script>

<template>
  <div class="app-shell">
    <Sidebar
      :collapsed="sidebarCollapsed"
      @toggle="sidebarCollapsed = !sidebarCollapsed"
      @palette="ui.showPalette = true"
      @workspaces="ui.showWorkspaces = true"
      @containers="ui.showContainers = true"
    />
    <div class="app-main">
      <slot :connected="connected" />
      <TerminalPanel />
      <StatusBar :connected="connected" @shortcuts="ui.openShortcuts()" />
    </div>
    <CommandPalette v-model="ui.showPalette" />
    <WorkspacePicker v-model="ui.showWorkspaces" />
    <ContainerMonitor v-model="ui.showContainers" />
    <InstructionsEditor v-model="ui.showInstructions" />
    <SystemPromptsManager v-model="ui.showSystemPrompts" />
    <TemplatesManager v-model="ui.showTemplates" />
    <KeyboardShortcutsModal v-model="ui.showShortcuts" />
    <ConfirmDialog />
    <Toaster />
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
