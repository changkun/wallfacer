<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, RouterLink } from 'vue-router';
import { ConsoleSidebar, type ConsoleNavModel } from 'latere-ui';
import 'latere-ui/console';
import { useTaskStore } from '../stores/tasks';
import { useAuthStore } from '../stores/auth';
import { useUiStore } from '../stores/ui';
import { useDialogStore } from '../stores/dialog';
import { useToastStore } from '../stores/toast';
import { api } from '../api/client';
import { derivePresence } from '../lib/presence';
import { hasUnseen } from '../lib/unread';
import { basename, groupLabel } from '../lib/workspaceLabel';

interface WorkspaceGroup {
  name?: string;
  workspaces: string[];
  key?: string;
}

const route = useRoute();
const store = useTaskStore();
const auth = useAuthStore();
const ui = useUiStore();
const dialog = useDialogStore();
const toast = useToastStore();

// Workspace group popover (multi-group create / switch / rename /
// delete). The "+ Manage workspaces" row still opens the path picker
// emit('workspaces') for the case where the user wants to compose a
// brand-new group from scratch.
const wsPopoverOpen = ref(false);
const switchingKey = ref('');
const workspaceGroups = computed<WorkspaceGroup[]>(
  () => store.config?.workspace_groups ?? [],
);

// Per-group running/waiting badge. The active group reads the live task list
// for instant updates; background groups use the server's active_groups info
// (refreshed via /api/config). Mirrors ui/js/workspace.js activeGroupBadgeHtml.
function groupBadge(g: WorkspaceGroup): { inProgress: number; waiting: number } {
  if (isActiveGroup(g)) {
    return { inProgress: store.inProgress.length, waiting: store.waiting.length };
  }
  const info = (store.config?.active_groups ?? []).find((a) => a.key === g.key);
  return { inProgress: info?.in_progress ?? 0, waiting: info?.waiting ?? 0 };
}
function activeKey(): string {
  return JSON.stringify(store.config?.workspaces ?? []);
}
function isActiveGroup(g: WorkspaceGroup): boolean {
  return JSON.stringify(g.workspaces) === activeKey();
}
async function switchToGroup(g: WorkspaceGroup) {
  if (isActiveGroup(g) || switchingKey.value) return;
  switchingKey.value = g.key ?? JSON.stringify(g.workspaces);
  try {
    await api('PUT', '/api/workspaces', { workspaces: g.workspaces });
    await Promise.all([store.fetchConfig(), store.fetchTasks({ includeArchived: ui.showArchived })]);
    toast.push(`Switched to ${g.name || 'workspace'}`, { kind: 'success' });
    wsPopoverOpen.value = false;
  } catch (e) {
    toast.push(`Switch failed: ${e instanceof Error ? e.message : String(e)}`, { kind: 'error' });
  } finally {
    switchingKey.value = '';
  }
}
function isSwitching(g: WorkspaceGroup): boolean {
  return switchingKey.value === (g.key ?? JSON.stringify(g.workspaces));
}
async function renameGroup(g: WorkspaceGroup) {
  const name = await dialog.prompt({
    title: 'Rename workspace',
    message: 'New name:',
    initial: g.name ?? '',
    placeholder: 'My workspace',
  });
  if (name == null) return;
  const next = workspaceGroups.value.map((x) =>
    x.key === g.key ? { ...x, name: name.trim() } : x,
  );
  await saveGroups(next, 'Renamed');
}
async function deleteGroup(g: WorkspaceGroup) {
  const ok = await dialog.confirm({
    title: 'Delete workspace',
    message: `Remove the ${g.name || 'unnamed'} workspace group? Tasks under it stay on disk but will no longer be reachable until the group is recreated.`,
    confirmLabel: 'Delete',
    danger: true,
  });
  if (!ok) return;
  const next = workspaceGroups.value.filter((x) => x.key !== g.key);
  await saveGroups(next, 'Deleted');
}
async function saveGroups(next: WorkspaceGroup[], verb: string) {
  try {
    await api('PUT', '/api/config', {
      workspace_groups: next.map(({ name, workspaces }) => ({ name, workspaces })),
    });
    await store.fetchConfig();
    toast.push(`${verb} workspace group`, { kind: 'success' });
  } catch (e) {
    toast.push(`${verb} failed: ${e instanceof Error ? e.message : String(e)}`, { kind: 'error' });
  }
}

defineProps<{ collapsed: boolean }>();
const emit = defineEmits<{ toggle: []; workspaces: []; palette: [] }>();

const cloudMode = computed(() => store.config?.cloud_mode === true);

const presence = computed(() => derivePresence(store.inProgress, auth.me));

// Board "unread" dot: set when new task ids arrive while off the board,
// cleared (and the seen-set refreshed) whenever the board is viewed.
const boardUnread = ref(false);
const seenTaskIds = new Set<string>();
function markBoardSeen() {
  seenTaskIds.clear();
  for (const t of store.tasks) seenTaskIds.add(t.id);
  boardUnread.value = false;
}
watch(
  () => store.tasks.map(t => t.id),
  (ids) => {
    if (route.path === '/') { markBoardSeen(); return; }
    if (hasUnseen(ids, seenTaskIds)) boardUnread.value = true;
  },
  { deep: true },
);
watch(() => route.path, (p) => { if (p === '/') markBoardSeen(); });

const activeWorkspaceLabel = computed(() => {
  const ws = store.config?.workspaces;
  if (!ws || ws.length === 0) return 'No workspace';
  const groups = store.config?.workspace_groups ?? [];
  const key = JSON.stringify(ws);
  const matched = groups.find(g => JSON.stringify(g.workspaces) === key);
  if (matched?.name) return matched.name;
  return ws.map(basename).join(' + ');
});

// The global nav, mapped onto the shared ConsoleSidebar model. Board carries an
// unread dot; Terminal is an action row (toggles the terminal panel, no route).
const navModel = computed<ConsoleNavModel>(() => ({
  groups: [
    { label: 'Workspace', items: [
      { id: 'board', label: 'Board', to: '/', icon: 'board', dot: boardUnread.value && route.path !== '/' },
      { id: 'plan', label: 'Plan', to: '/plan', icon: 'plan' },
      { id: 'agents', label: 'Agents', to: '/agents', icon: 'agents' },
      { id: 'flows', label: 'Flows', to: '/flows', icon: 'flows' },
      { id: 'routines', label: 'Routines', to: '/routines', icon: 'routines' },
      { id: 'map', label: 'Map', to: '/map', icon: 'map' },
    ] },
    { label: 'Inspect', items: [
      { id: 'terminal', label: 'Terminal', action: true, icon: 'terminal' },
      { id: 'analytics', label: 'Analytics', to: '/analytics', icon: 'analytics' },
    ] },
    { pin: 'bottom', items: [
      { id: 'docs', label: 'Docs', to: '/docs', icon: 'docs' },
      { id: 'settings', label: 'Settings', to: '/settings', icon: 'settings' },
    ] },
  ],
}));

const activeNav = computed(() => {
  const p = route.path;
  if (p === '/') return 'board';
  if (p.startsWith('/docs')) return 'docs';
  return p.slice(1).split('/')[0];
});

function onNavigate(item: { id: string }) {
  // Routed rows navigate via RouterLink; the Terminal action row toggles here.
  if (item.id === 'terminal') ui.toggleTerminal();
}

onMounted(() => {
  if (cloudMode.value && !auth.loaded) void auth.fetchMe();
  if (route.path === '/') markBoardSeen();
});

// Click outside the workspace popover closes it. Only attach the listener
// while open so we're not doing global work for every click on the page.
watch(wsPopoverOpen, (open) => {
  if (!open) return;
  const handler = (e: MouseEvent) => {
    const wrap = (e.target as HTMLElement).closest('.sb-ws-switch-wrap');
    if (!wrap) {
      wsPopoverOpen.value = false;
      document.removeEventListener('mousedown', handler);
    }
  };
  setTimeout(() => document.addEventListener('mousedown', handler), 0);
});
</script>

<template>
  <ConsoleSidebar
    class="wf-cs"
    :class="{ collapsed }"
    :model="navModel"
    :active-key="activeNav"
    :collapsed="collapsed"
    :router-link="RouterLink"
    brand-name="Wallfacer"
    brand-sub="Workspace"
    expand-on-brand-click
    search
    search-label="Search or command"
    @navigate="onNavigate"
    @search="emit('palette')"
    @update:collapsed="emit('toggle')"
  >
    <template #logo>
      <span class="sb-logo" aria-hidden="true">
        <svg width="20" height="20" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" style="display:block;image-rendering:pixelated">
          <rect x="0" y="0" width="6" height="3" fill="var(--accent)" />
          <rect x="7" y="0" width="9" height="3" fill="var(--accent-2)" />
          <rect x="0" y="4" width="4" height="3" fill="#8a3e21" />
          <rect x="5" y="4" width="6" height="3" fill="var(--accent)" />
          <rect x="12" y="4" width="4" height="3" fill="var(--accent-2)" />
          <rect x="0" y="8" width="7" height="3" fill="var(--accent-2)" />
          <rect x="8" y="8" width="8" height="3" fill="#8a3e21" />
          <rect x="0" y="12" width="3" height="4" fill="var(--accent)" />
          <rect x="4" y="12" width="6" height="4" fill="#8a3e21" />
          <rect x="11" y="12" width="5" height="4" fill="var(--accent)" />
        </svg>
      </span>
    </template>

    <!-- Workspace switcher + command palette, above the nav -->
    <template #top>
      <template v-if="!collapsed">
        <div class="sb-ws-switch-wrap">
          <button
            type="button"
            class="sb-ws-switch"
            title="Switch workspace group"
            :aria-expanded="wsPopoverOpen"
            @click="wsPopoverOpen = !wsPopoverOpen"
          >
            <span class="ws-dot">W</span>
            <span class="ws-name">{{ activeWorkspaceLabel }}</span>
            <span class="ws-caret">
              <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <polyline points="6 9 12 15 18 9"></polyline>
              </svg>
            </span>
          </button>
          <div
            v-if="wsPopoverOpen"
            class="sb-ws-popover sb-ws-popover--inline"
            role="menu"
            @click.stop
          >
            <button
              v-for="g in workspaceGroups"
              :key="g.key ?? g.workspaces.join('|')"
              type="button"
              class="sb-ws-popover__item"
              :class="{ active: isActiveGroup(g) }"
              role="menuitem"
              :title="g.workspaces.join(', ')"
              @click="switchToGroup(g)"
            >
              <span class="sb-ws-popover__check">{{ isActiveGroup(g) ? '✓' : '' }}</span>
              <span class="sb-ws-popover__label">{{ groupLabel(g) }}</span>
              <span v-if="isSwitching(g)" class="sb-ws-popover__switching">switching…</span>
              <span v-else class="sb-ws-popover__counts">
                <span v-if="groupBadge(g).inProgress > 0" class="badge badge-in_progress" :title="`${groupBadge(g).inProgress} running`">{{ groupBadge(g).inProgress }}</span>
                <span v-if="groupBadge(g).waiting > 0" class="badge badge-waiting" :title="`${groupBadge(g).waiting} waiting`">{{ groupBadge(g).waiting }}</span>
              </span>
              <span class="sb-ws-popover__row-actions">
                <button type="button" class="sb-ws-popover__row-btn" :title="`Rename ${g.name || 'workspace'}`" @click.stop="renameGroup(g)">✎</button>
                <button type="button" class="sb-ws-popover__row-btn" :title="`Delete ${g.name || 'workspace'}`" @click.stop="deleteGroup(g)">×</button>
              </span>
            </button>
            <div class="sb-ws-popover__divider" />
            <button
              type="button"
              class="sb-ws-popover__item sb-ws-popover__add"
              role="menuitem"
              @click="wsPopoverOpen = false; emit('workspaces')"
            >
              <span class="sb-ws-popover__check">+</span>
              <span class="sb-ws-popover__label">Add workspace…</span>
            </button>
          </div>
        </div>
      </template>
    </template>

    <!-- Per-item icons -->
    <template #icon="{ item }">
      <svg v-if="item.icon === 'board'" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="7" height="9" rx="1"></rect><rect x="14" y="3" width="7" height="5" rx="1"></rect><rect x="14" y="12" width="7" height="9" rx="1"></rect><rect x="3" y="16" width="7" height="5" rx="1"></rect></svg>
      <svg v-else-if="item.icon === 'plan'" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><polyline points="14 2 14 8 20 8"></polyline><line x1="16" y1="13" x2="8" y2="13"></line><line x1="16" y1="17" x2="8" y2="17"></line></svg>
      <svg v-else-if="item.icon === 'agents'" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="8" r="4"></circle><path d="M4 21c0-4 4-7 8-7s8 3 8 7"></path></svg>
      <svg v-else-if="item.icon === 'flows'" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="5" cy="6" r="2"></circle><circle cx="5" cy="18" r="2"></circle><circle cx="19" cy="12" r="2"></circle><path d="M7 6h6a4 4 0 0 1 4 4v2"></path><path d="M7 18h6a4 4 0 0 0 4-4v-2"></path></svg>
      <svg v-else-if="item.icon === 'routines'" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="9"></circle><polyline points="12 7 12 12 15 14"></polyline></svg>
      <svg v-else-if="item.icon === 'map'" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="5" cy="6" r="2"></circle><circle cx="19" cy="6" r="2"></circle><circle cx="12" cy="18" r="2"></circle><line x1="5" y1="8" x2="12" y2="16"></line><line x1="19" y1="8" x2="12" y2="16"></line></svg>
      <svg v-else-if="item.icon === 'terminal'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><polyline points="4 17 10 11 4 5"></polyline><line x1="12" y1="19" x2="20" y2="19"></line></svg>
      <svg v-else-if="item.icon === 'analytics'" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="18" y="3" width="4" height="18"></rect><rect x="10" y="8" width="4" height="13"></rect><rect x="2" y="13" width="4" height="8"></rect></svg>
      <svg v-else-if="item.icon === 'docs'" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"></path><path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"></path></svg>
      <svg v-else width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"></path></svg>
    </template>

    <!-- Presence: one entry per running agent + the signed-in user -->
    <template #extra>
      <div v-if="!collapsed && presence.length" class="sb-presence">
        <div class="sb-presence-label">Presence</div>
        <div
          v-for="p in presence"
          :key="p.id"
          class="sb-presence-item"
          :class="'sb-presence-item--' + p.kind"
          :title="p.kind === 'agent' ? 'Running agent' : 'You'"
        >
          <span class="sb-presence-dot" aria-hidden="true" />
          <span class="sb-presence-name">{{ p.label }}</span>
        </div>
      </div>
    </template>

    <!-- Cloud-mode account chip (only when /api/me responds) -->
    <template #foot>
      <a
        v-if="cloudMode && auth.me"
        class="sb-account"
        :href="auth.me.auth_url ? auth.me.auth_url + '/me' : '#'"
        target="_blank"
        rel="noopener"
        :title="auth.me.email"
      >
        <img v-if="auth.me.picture" class="sb-account-avatar" :src="auth.me.picture" alt="" />
        <span v-else class="sb-account-avatar sb-account-avatar--mono">
          {{ (auth.me.name || auth.me.email || '?').slice(0, 1).toUpperCase() }}
        </span>
        <span class="sb-account-text">
          <span class="sb-account-name">{{ auth.me.name || auth.me.email }}</span>
          <span class="sb-account-meta">Signed in</span>
        </span>
      </a>
      <a
        v-else-if="cloudMode && auth.loaded && !auth.me && store.config?.workspaces"
        class="sb-account sb-account--signin"
        href="/login"
      >
        <span class="sb-account-avatar sb-account-avatar--mono">→</span>
        <span class="sb-account-text">
          <span class="sb-account-name">Sign in</span>
          <span class="sb-account-meta">Not signed in</span>
        </span>
      </a>
    </template>
  </ConsoleSidebar>
</template>

<style scoped>
/* wallfacer sizes its own rail via --sb-w / --sb-w-icon (it's a flex child of
 * .app-shell, not a grid column), so drive width from those tokens. */
.wf-cs {
  width: var(--sb-w) !important;
}
.wf-cs.collapsed {
  width: var(--sb-w-icon) !important;
}
/* Match the workspace switcher to the search bar below it: full width + the
 * same height/radius, so they read as one consistent stack. */
.wf-cs :deep(.sb-ws-switch) {
  width: 100%;
  margin: 6px 0 0;
  min-height: 38px;
  border-radius: 9px;
}
.wf-cs :deep(.sb-ws-switch-wrap) {
  width: 100%;
}
</style>
