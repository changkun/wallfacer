<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted, watch } from 'vue';
import { useHead } from '@unhead/vue';
import { useRoute, useRouter } from 'vue-router';
import Sidebar from '../components/Sidebar.vue';
import StatusBar from '../components/StatusBar.vue';
import CommandPalette from '../components/CommandPalette.vue';
import WorkspacePicker from '../components/WorkspacePicker.vue';
import WorkspaceEditModal from '../components/WorkspaceEditModal.vue';
import SystemPromptsManager from '../components/SystemPromptsManager.vue';
import DockWorkspace from '../components/DockWorkspace.vue';
import KeyboardShortcutsModal from '../components/KeyboardShortcutsModal.vue';
import ConfirmDialog from '../components/ConfirmDialog.vue';
import Toaster from '../components/Toaster.vue';
import SpecChatPopup from '../components/plan/SpecChatPopup.vue';
import { useSse } from '../composables/useSse';
import { useTaskStore } from '../stores/tasks';
import { useWorkspacesStore } from '../stores/workspaces';
import { useUiStore } from '../stores/ui';
import { useKeyboard } from '../composables/useKeyboard';
import { getStored, setStored } from '../lib/storage';
import { shouldRefetchOnVisible } from '../lib/visibility';
import type { Task } from '../api/types';

const store = useTaskStore();
const workspaces = useWorkspacesStore();
const ui = useUiStore();
const router = useRouter();
const route = useRoute();
// Sidebar collapse persists across refreshes — losing it on every reload
// is a real micro-regression vs the legacy wallfacer-sidebar-collapsed.
const SIDEBAR_KEY = 'wallfacer-sidebar-collapsed';
const sidebarCollapsed = ref<boolean>(getStored(SIDEBAR_KEY) === '1');
watch(sidebarCollapsed, (v) => setStored(SIDEBAR_KEY, v ? '1' : '0'));

// The floating agent-session chat popup is available app-wide so chat can be
// triggered from any tab (board, agents, flows, …). The dedicated Chat tab and
// the Plan view own their own chat surface, so the global popup stands down on
// those routes to avoid two chat sessions racing on the shared agent store.
const CHAT_OWNING_ROUTES = ['/chat', '/plan'];
const showChatPopup = computed(() =>
  !CHAT_OWNING_ROUTES.some((p) => router.currentRoute.value.path.startsWith(p)),
);

onMounted(async () => {
  // Capture the deep-link target BEFORE any await: the activeId watcher below
  // fires the moment fetchConfig resolves (activeId flips '' -> server value)
  // and rewrites the URL, which would wipe ?ws before it can be read.
  const incomingWs = typeof route.query.ws === 'string' ? route.query.ws : '';
  if (!store.config) await store.fetchConfig();
  // Load the first-class workspace registry so the status bar and picker can
  // show workspace names (not just folder basenames), and so the deep-link
  // target can be validated against known ids. Awaited (not fire-and-forget)
  // so the activate-from-URL below sees a populated registry.
  await workspaces.list();
  // Deep link: if ?ws=<id> names a known workspace that isn't already active,
  // jump to it. Unknown/invalid ids are ignored (no crash). Runs once on load.
  if (
    incomingWs &&
    incomingWs !== workspaces.activeId &&
    workspaces.workspaces.some(w => w.id === incomingWs)
  ) {
    try { await workspaces.activate(incomingWs); } catch { /* ignore bad deep link */ }
  }
  // First-run guard: when the server has no active workspace yet, open
  // the picker automatically so the user can wire one up before tasks
  // fail with "no workspace selected" errors downstream. Mirrors the
  // legacy ui/js/workspace.js workspacePickerRequired path.
  if (!(store.config?.workspaces?.length)) {
    ui.showWorkspaces = true;
  }
});

// Keep ?ws=<id> in the URL in sync with the active workspace so a copied link
// reopens the same project (important in cloud mode). router.replace avoids
// polluting history; the equality guard prevents a feedback loop with the
// activate-from-URL path above.
watch(() => workspaces.activeId, (id) => {
  if (!id) return;
  if (route.query.ws === id) return;
  void router.replace({ path: route.path, query: { ...route.query, ws: id } });
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

const { connected, connState } = useSse({
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
  onExplorer: () => {
    // The explorer is an in-board panel now; jump to the board first if we're
    // on another route, then toggle it.
    if (router.currentRoute.value.path === '/') ui.toggleExplorer();
    else { void router.push('/'); ui.openExplorer(); }
  },
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
      <DockWorkspace>
        <slot :connected="connected" :conn-state="connState" />
      </DockWorkspace>
      <StatusBar :connected="connected" :conn-state="connState" @shortcuts="ui.openShortcuts()" />
    </div>
    <CommandPalette v-model="ui.showPalette" />
    <WorkspacePicker v-model="ui.showWorkspaces" />
    <WorkspaceEditModal v-if="ui.editWorkspaceId" />
    <SystemPromptsManager v-model="ui.showSystemPrompts" />
    <KeyboardShortcutsModal v-model="ui.showShortcuts" />
    <ConfirmDialog />
    <Toaster />
    <SpecChatPopup v-if="showChatPopup" />

    <!-- Full-UI blocking overlay during a workspace switch. Rendered as the last
         child of .app-shell so it stacks above every modal (WorkspacePicker
         z-50) and the collapsed sidebar popover (z-200). Keeps the user from
         seeing the new active state painted over stale old content mid-switch. -->
    <div
      v-if="ui.switchingWorkspace"
      class="ws-switch-overlay"
      role="status"
      aria-live="polite"
      aria-busy="true"
    >
      <div class="ws-switch-overlay__panel">
        <span class="ws-switch-overlay__spinner" aria-hidden="true" />
        <span class="ws-switch-overlay__label">Switching workspace…</span>
      </div>
    </div>
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
  /* Scroll overflowing page content instead of clipping it. Height-constrained
   * pages (flex:1 + min-height:0 chains down to their own overflow-y:auto body)
   * don't overflow this column, so they get no second scrollbar; a page that
   * spills past the viewport becomes reachable instead of being clipped. */
  overflow-y: auto;
  overflow-x: hidden;
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
/* Sits above every modal (z-50) and the collapsed sidebar popover (z-200). */
.ws-switch-overlay {
  position: fixed;
  inset: 0;
  z-index: 1000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in oklab, var(--bg) 55%, transparent);
  backdrop-filter: blur(4px);
  -webkit-backdrop-filter: blur(4px);
}
.ws-switch-overlay__panel {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  color: var(--ink);
}
.ws-switch-overlay__spinner {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  border: 3px solid color-mix(in oklab, var(--ink) 20%, transparent);
  border-top-color: var(--accent);
  animation: ws-switch-spin 0.7s linear infinite;
}
.ws-switch-overlay__label {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.2px;
}
@keyframes ws-switch-spin {
  to { transform: rotate(360deg); }
}
</style>
