<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { useRoute } from 'vue-router';
import { useTaskStore } from '../stores/tasks';
import { useAuthStore } from '../stores/auth';
import { useUiStore } from '../stores/ui';

const route = useRoute();
const store = useTaskStore();
const auth = useAuthStore();
const ui = useUiStore();

const props = defineProps<{ collapsed: boolean }>();
const emit = defineEmits<{ toggle: []; workspaces: []; containers: []; palette: [] }>();

const cloudMode = computed(() => store.config?.cloud_mode === true);

const activeWorkspaceLabel = computed(() => {
  const ws = store.config?.workspaces;
  if (!ws || ws.length === 0) return 'No workspace';
  const groups = store.config?.workspace_groups ?? [];
  const key = JSON.stringify(ws);
  const matched = groups.find(g => JSON.stringify(g.workspaces) === key);
  if (matched?.name) return matched.name;
  if (ws.length === 1) {
    const parts = ws[0].replace(/\/+$/, '').split('/');
    return parts[parts.length - 1] || ws[0];
  }
  return ws.map(p => {
    const parts = p.replace(/\/+$/, '').split('/');
    return parts[parts.length - 1] || p;
  }).join(' + ');
});

function onBrandClick() {
  if (props.collapsed) emit('toggle');
}

onMounted(() => {
  if (cloudMode.value && !auth.loaded) void auth.fetchMe();
});
</script>

<template>
  <aside class="app-sidebar" :class="{ collapsed }">
    <!-- Brand: clickable when collapsed to unfold -->
    <div
      class="sb-brand"
      :class="{ 'is-collapsed-toggle': collapsed }"
      :title="collapsed ? 'Expand sidebar' : ''"
      @click="onBrandClick"
    >
      <span class="sb-logo" aria-hidden="true">
        <svg width="22" height="22" viewBox="0 0 16 16" fill="none" xmlns="http://www.w3.org/2000/svg" style="display:block;image-rendering:pixelated">
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
      <a
        href="https://github.com/changkun/wallfacer"
        target="_blank"
        rel="noopener noreferrer"
        class="sb-brand-name"
        @click.stop
      >Wallfacer</a>
      <button
        v-if="!collapsed"
        type="button"
        class="sb-collapse"
        title="Collapse sidebar"
        @click.stop="emit('toggle')"
      >
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
          <line x1="9" y1="3" x2="9" y2="21"></line>
        </svg>
      </button>
    </div>

    <!-- Workspace group switcher -->
    <button type="button" class="sb-ws-switch" title="Switch workspace group" @click="emit('workspaces')">
      <span class="ws-dot">W</span>
      <span class="ws-name">{{ activeWorkspaceLabel }}</span>
      <span class="ws-caret">
        <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="6 9 12 15 18 9"></polyline>
        </svg>
      </span>
    </button>

    <!-- Search / command palette -->
    <button type="button" class="sb-search" title="Search (&#x2318;K)" @click="emit('palette')">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
        <circle cx="11" cy="11" r="7"></circle>
        <line x1="20" y1="20" x2="16.65" y2="16.65"></line>
      </svg>
      <span>Search or command</span>
      <span class="kbd">&#x2318;K</span>
    </button>

    <!-- Workspace section -->
    <div class="sb-section">Workspace</div>
    <div class="sb-nav">
      <router-link to="/" class="sb-nav-item" :class="{ active: route.path === '/' }">
        <span class="sb-icon">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
            <rect x="3" y="3" width="7" height="9" rx="1"></rect>
            <rect x="14" y="3" width="7" height="5" rx="1"></rect>
            <rect x="14" y="12" width="7" height="9" rx="1"></rect>
            <rect x="3" y="16" width="7" height="5" rx="1"></rect>
          </svg>
        </span>
        <span>Board</span>
      </router-link>

      <router-link to="/plan" class="sb-nav-item" :class="{ active: route.path === '/plan' }">
        <span class="sb-icon">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
            <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path>
            <polyline points="14 2 14 8 20 8"></polyline>
            <line x1="16" y1="13" x2="8" y2="13"></line>
            <line x1="16" y1="17" x2="8" y2="17"></line>
          </svg>
        </span>
        <span>Plan</span>
      </router-link>

      <router-link to="/agents" class="sb-nav-item" :class="{ active: route.path === '/agents' }">
        <span class="sb-icon">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="12" cy="8" r="4"></circle>
            <path d="M4 21c0-4 4-7 8-7s8 3 8 7"></path>
          </svg>
        </span>
        <span>Agents</span>
      </router-link>

      <router-link to="/flows" class="sb-nav-item" :class="{ active: route.path === '/flows' }">
        <span class="sb-icon">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="5" cy="6" r="2"></circle>
            <circle cx="5" cy="18" r="2"></circle>
            <circle cx="19" cy="12" r="2"></circle>
            <path d="M7 6h6a4 4 0 0 1 4 4v2"></path>
            <path d="M7 18h6a4 4 0 0 0 4-4v-2"></path>
          </svg>
        </span>
        <span>Flows</span>
      </router-link>

      <router-link to="/map" class="sb-nav-item" :class="{ active: route.path === '/map' }" title="Dependency map">
        <span class="sb-icon">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="5" cy="6" r="2"></circle>
            <circle cx="19" cy="6" r="2"></circle>
            <circle cx="12" cy="18" r="2"></circle>
            <line x1="5" y1="8" x2="12" y2="16"></line>
            <line x1="19" y1="8" x2="12" y2="16"></line>
          </svg>
        </span>
        <span>Map</span>
      </router-link>
    </div>

    <!-- Inspect section -->
    <div class="sb-section">Inspect</div>
    <div class="sb-nav">
      <button
        type="button"
        class="sb-nav-item"
        :class="{ active: ui.showTerminal }"
        @click="ui.toggleTerminal()"
      >
        <span class="sb-icon">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="4 17 10 11 4 5"></polyline>
            <line x1="12" y1="19" x2="20" y2="19"></line>
          </svg>
        </span>
        <span>Terminal</span>
        <span class="kbd">&#x2303;`</span>
      </button>

      <router-link to="/analytics" class="sb-nav-item" :class="{ active: route.path === '/analytics' }">
        <span class="sb-icon">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <rect x="18" y="3" width="4" height="18"></rect>
            <rect x="10" y="8" width="4" height="13"></rect>
            <rect x="2" y="13" width="4" height="8"></rect>
          </svg>
        </span>
        <span>Analytics</span>
      </router-link>
    </div>

    <div class="sb-spacer"></div>

    <!-- Bottom nav: Docs, Settings, optional account chip -->
    <div class="sb-divider"></div>
    <div class="sb-nav">
      <router-link to="/docs" class="sb-nav-item" :class="{ active: route.path.startsWith('/docs') }" title="Docs">
        <span class="sb-icon">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
            <path d="M2 3h6a4 4 0 0 1 4 4v14a3 3 0 0 0-3-3H2z"></path>
            <path d="M22 3h-6a4 4 0 0 0-4 4v14a3 3 0 0 1 3-3h7z"></path>
          </svg>
        </span>
        <span>Docs</span>
      </router-link>

      <router-link to="/settings" class="sb-nav-item" :class="{ active: route.path === '/settings' }" title="Settings">
        <span class="sb-icon">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="12" cy="12" r="3"></circle>
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"></path>
          </svg>
        </span>
        <span>Settings</span>
      </router-link>
    </div>

    <!-- Cloud-mode account chip (only when /api/me responds) -->
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
  </aside>
</template>
