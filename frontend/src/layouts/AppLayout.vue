<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted, watch } from 'vue';
import { useHead } from '@unhead/vue';
import { useRouter } from 'vue-router';
import Sidebar from '../components/Sidebar.vue';
import StatusBar from '../components/StatusBar.vue';
import CommandPalette from '../components/CommandPalette.vue';
import WorkspacePicker from '../components/WorkspacePicker.vue';
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
import { getStored, setStored } from '../lib/storage';
import { shouldRefetchOnVisible } from '../lib/visibility';
import type { Task } from '../api/types';

const store = useTaskStore();
const ui = useUiStore();
const router = useRouter();
// Sidebar collapse persists across refreshes — losing it on every reload
// is a real micro-regression vs the legacy wallfacer-sidebar-collapsed.
const SIDEBAR_KEY = 'wallfacer-sidebar-collapsed';
const sidebarCollapsed = ref<boolean>(getStored(SIDEBAR_KEY) === '1');
watch(sidebarCollapsed, (v) => setStored(SIDEBAR_KEY, v ? '1' : '0'));

onMounted(async () => {
  if (!store.config) await store.fetchConfig();
  // First-run guard: when the server has no active workspace yet, open
  // the picker automatically so the user can wire one up before tasks
  // fail with "no workspace selected" errors downstream. Mirrors the
  // legacy ui/js/workspace.js workspacePickerRequired path.
  if (!(store.config?.workspaces?.length)) {
    ui.showWorkspaces = true;
  }
});

// On tab refocus, refetch the task list so any SSE events missed while the
// tab was hidden are picked up immediately (legacy ui/js/api.js fallback).
function onVisibilityChange() {
  if (shouldRefetchOnVisible(document.visibilityState, !!store.config?.workspaces?.length)) {
    void store.fetchTasks({ includeArchived: ui.showArchived });
  }
}
onMounted(() => document.addEventListener('visibilitychange', onVisibilityChange));
onUnmounted(() => document.removeEventListener('visibilitychange', onVisibilityChange));

// Browser-tab title reflects the active workspace + the count of running
// tasks. "Wallfacer" alone when nothing's running; "Wallfacer — repo (3)"
// when three tasks are in progress. Mirrors legacy ui/js/git.js title
// updates.
useHead(computed(() => {
  const ws = store.config?.workspaces ?? [];
  const wsLabel = ws.length === 0
    ? ''
    : ws.length === 1
      ? ws[0].replace(/\/+$/, '').split('/').pop() || ws[0]
      : `${ws.length} workspaces`;
  const running = store.inProgress.length;
  const parts: string[] = ['Wallfacer'];
  if (wsLabel) parts.push(wsLabel);
  const title = parts.join(' — ') + (running > 0 ? ` (${running})` : '');
  return { title };
}));

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

// Show an obvious banner the moment the SSE stream goes down so the
// user can't mistake stale data for live data. Hold a 1 s grace
// period before showing — fleeting tab focus changes shouldn't flash
// the banner — and hide immediately on reconnect.
const showDisconnectBanner = ref(false);
let disconnectTimer: ReturnType<typeof setTimeout> | null = null;
watch(connected, (now) => {
  if (now) {
    if (disconnectTimer) { clearTimeout(disconnectTimer); disconnectTimer = null; }
    showDisconnectBanner.value = false;
    return;
  }
  if (disconnectTimer) return;
  disconnectTimer = setTimeout(() => {
    showDisconnectBanner.value = true;
    disconnectTimer = null;
  }, 1000);
});

useKeyboard({
  onSearch: () => { ui.showPalette = true; },
  onFocusSearch: () => document.querySelector<HTMLInputElement>('.task-search-input')?.focus(),
  onNewTask: () => document.querySelector<HTMLTextAreaElement>('.composer-input')?.focus(),
  onSettings: () => { void router.push('/settings'); },
  onTerminal: () => { ui.toggleTerminal(); },
  onShortcuts: () => { ui.openShortcuts(); },
  onExplorer: () => { void router.push('/explorer'); },
  onToggleMode: () => { void router.push(router.currentRoute.value.path.startsWith('/plan') ? '/' : '/plan'); },
});
</script>

<template>
  <div class="app-shell">
    <Sidebar
      :collapsed="sidebarCollapsed"
      @toggle="sidebarCollapsed = !sidebarCollapsed"
      @palette="ui.showPalette = true"
      @workspaces="ui.showWorkspaces = true"
    />
    <div class="app-main">
      <div
        v-if="showDisconnectBanner"
        class="app-disconnected-banner"
        role="status"
        aria-live="polite"
      >
        <span aria-hidden="true">⚠</span>
        Live updates paused — server unreachable. Reconnecting…
      </div>
      <slot :connected="connected" />
      <TerminalPanel />
      <StatusBar :connected="connected" @shortcuts="ui.openShortcuts()" />
    </div>
    <CommandPalette v-model="ui.showPalette" />
    <WorkspacePicker v-model="ui.showWorkspaces" />
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
.app-disconnected-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 14px;
  background: color-mix(in oklab, var(--warn, #c87b1c) 18%, var(--bg-card));
  color: var(--ink);
  border-bottom: 1px solid var(--border);
  font-size: 12px;
}
</style>
