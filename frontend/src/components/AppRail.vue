<script setup lang="ts">
// The console rail: brand row, workspace chip, palette trigger, nav groups,
// presence, account. Wallfacer owns this chrome rather than composing the
// shared package rail; the geometry is the design system's nav row and
// the rail sits on the deep ground with no border, so the seam between it and
// the content card is a depth, not a line. See specs/shared/console-redesign/shell.md.
import { computed, onMounted, ref, watch } from 'vue';
import { RouterLink, useRoute } from 'vue-router';
import { ProductSwitcher } from 'latere-ui';
import AccountControl from './AccountControl.vue';
import WorkspaceChip from './WorkspaceChip.vue';
import NavIcon from './NavIcon.vue';
import { useTaskStore } from '../stores/tasks';
import { useAuthStore } from '../stores/auth';
import { useUiStore } from '../stores/ui';
import { derivePresence } from '../lib/presence';
import { hasUnseen } from '../lib/unread';
import { NAV_GROUPS, activeNavId, type NavItem } from '../lib/nav';

const props = defineProps<{
  collapsed: boolean;
  connState: 'ok' | 'reconnecting' | 'closed';
}>();
const emit = defineEmits<{ toggle: []; workspaces: []; palette: [] }>();

const route = useRoute();
const store = useTaskStore();
const auth = useAuthStore();
const ui = useUiStore();

// Sign-in is available whenever the server wired an OIDC client (the default
// for `wallfacer run`). Drives the account control at the foot.
const authEnabled = computed(() => store.config?.auth_enabled === true);
const presence = computed(() => derivePresence(store.inProgress, auth.me));

// The Board row carries the live count (running + waiting) the status bar used
// to show, and an unread dot when new tasks arrived while the board was not on
// screen and nothing is running.
const boardCount = computed(() => store.inProgress.length + store.waiting.length);
const boardUnread = ref(false);
const seenTaskIds = new Set<string>();
function markBoardSeen() {
  seenTaskIds.clear();
  for (const t of store.tasks) seenTaskIds.add(t.id);
  boardUnread.value = false;
}
watch(
  () => store.tasks.map((t) => t.id),
  (ids) => {
    if (route.path === '/') { markBoardSeen(); return; }
    if (hasUnseen(ids, seenTaskIds)) boardUnread.value = true;
  },
  { deep: true },
);
watch(() => route.path, (p) => { if (p === '/') markBoardSeen(); });
onMounted(() => { if (route.path === '/') markBoardSeen(); });

const activeId = computed(() => activeNavId(route.path));

function onAction(item: NavItem) {
  if (item.id === 'terminal') ui.toggleTerminal();
}
function onNavigate() {
  // A tap on a drawer row closes the drawer; a click on the desktop rail is a no-op.
  ui.closeRail();
}

watch(
  authEnabled,
  (enabled) => { if (enabled && !auth.loaded) void auth.fetchMe(); },
  { immediate: true },
);
</script>

<template>
  <aside class="app-rail" :class="{ 'app-rail--fold': collapsed, 'app-rail--open': ui.railOpen }" aria-label="Navigation">
    <div class="rail-head">
      <RouterLink to="/" class="rail-brand" title="Wallfacer" @click="onNavigate">
        <span class="rail-mark" aria-hidden="true">
          <svg width="20" height="20" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" style="display:block;image-rendering:pixelated">
            <rect x="0" y="0" width="6" height="3" fill="var(--accent)" />
            <rect x="7" y="0" width="9" height="3" fill="var(--accent-2)" />
            <rect x="0" y="4" width="4" height="3" fill="var(--accent-2)" />
            <rect x="5" y="4" width="6" height="3" fill="var(--accent)" />
            <rect x="12" y="4" width="4" height="3" fill="var(--accent-2)" />
            <rect x="0" y="8" width="7" height="3" fill="var(--accent-2)" />
            <rect x="8" y="8" width="8" height="3" fill="var(--accent-2)" />
            <rect x="0" y="12" width="3" height="4" fill="var(--accent)" />
            <rect x="4" y="12" width="6" height="4" fill="var(--accent-2)" />
            <rect x="11" y="12" width="5" height="4" fill="var(--accent)" />
          </svg>
        </span>
        <span class="rail-name wallfacer-brand">Wallfacer</span>
      </RouterLink>
      <ProductSwitcher v-if="!collapsed" current="wallfacer" size="sm" class="rail-products" />
      <button
        type="button"
        class="icon-btn sm rail-fold-btn"
        :title="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
        :aria-label="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
        :aria-expanded="!collapsed"
        @click="emit('toggle')"
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <rect x="3" y="4" width="18" height="16" rx="3"></rect>
          <line x1="9" y1="4" x2="9" y2="20"></line>
        </svg>
      </button>
    </div>

    <WorkspaceChip :collapsed="collapsed" :conn-state="props.connState" @workspaces="emit('workspaces')" />

    <button
      type="button"
      class="rail-search"
      title="Search or command (⌘K)"
      aria-label="Search or command"
      @click="emit('palette')"
    >
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <circle cx="11" cy="11" r="8"></circle>
        <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
      </svg>
      <span class="rail-search-label">Search or command</span>
      <kbd class="key rail-search-hint">⌘K</kbd>
    </button>

    <nav class="rail-nav" aria-label="Sections">
      <template v-for="group in NAV_GROUPS" :key="group.label ?? 'bottom'">
        <div class="rail-group" :class="{ 'rail-group--bottom': group.pin === 'bottom' }">
          <div v-if="group.label" class="eyebrow rail-eyebrow">{{ group.label }}</div>
          <template v-for="item in group.items" :key="item.id">
            <RouterLink
              v-if="item.to"
              :to="item.to"
              class="nav-btn"
              :class="{ active: activeId === item.id }"
              :aria-current="activeId === item.id ? 'page' : undefined"
              :title="collapsed ? item.label : undefined"
              :data-nav="item.id"
              @click="onNavigate"
            >
              <span class="nav-ico"><NavIcon :name="item.icon" /></span>
              <span class="nav-label">{{ item.label }}</span>
              <span v-if="item.id === 'board' && boardCount > 0" class="count nav-count" :title="`${store.inProgress.length} running, ${store.waiting.length} waiting`">{{ boardCount }}</span>
              <span v-else-if="item.id === 'board' && boardUnread && activeId !== 'board'" class="nav-dot" aria-label="New tasks" />
            </RouterLink>
            <button
              v-else
              type="button"
              class="nav-btn"
              :class="{ on: item.id === 'terminal' && ui.showTerminal }"
              :aria-pressed="item.id === 'terminal' ? ui.showTerminal : undefined"
              :title="collapsed ? item.label : undefined"
              :data-nav="item.id"
              @click="onAction(item)"
            >
              <span class="nav-ico"><NavIcon :name="item.icon" /></span>
              <span class="nav-label">{{ item.label }}</span>
            </button>
          </template>
        </div>
      </template>

      <div v-if="!collapsed && presence.length" class="rail-presence" aria-label="Presence">
        <div class="eyebrow rail-eyebrow">Presence</div>
        <div
          v-for="p in presence"
          :key="p.id"
          class="rail-presence-item"
          :class="'rail-presence-item--' + p.kind"
          :title="p.kind === 'agent' ? 'Running agent' : 'You'"
        >
          <span class="pill-dot" aria-hidden="true" />
          <span class="rail-presence-name">{{ p.label }}</span>
        </div>
      </div>
    </nav>

    <div class="rail-foot">
      <AccountControl v-if="authEnabled" placement="bottom-start" />
    </div>
  </aside>
</template>
