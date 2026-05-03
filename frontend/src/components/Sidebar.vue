<script setup lang="ts">
import { useRoute } from 'vue-router';
import { useTaskStore } from '../stores/tasks';
import { useTheme } from '../composables/useTheme';

const route = useRoute();

const store = useTaskStore();
const { theme, cycle } = useTheme();

defineProps<{ collapsed: boolean }>();
const emit = defineEmits<{ toggle: []; settings: []; workspaces: []; containers: []; palette: [] }>();

function themeLabel(): string {
  switch (theme.value) {
    case 'light': return 'Light';
    case 'dark': return 'Dark';
    default: return 'Auto';
  }
}
</script>

<template>
  <aside class="app-sidebar" :class="{ collapsed }">
    <!-- Brand -->
    <div class="sb-brand">
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
      <span class="sb-brand-name">Wallfacer</span>
      <button type="button" class="sb-collapse" title="Collapse sidebar" @click.stop="emit('toggle')">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
          <line x1="9" y1="3" x2="9" y2="21"></line>
        </svg>
      </button>
    </div>

    <!-- Workspace group switcher -->
    <button type="button" class="sb-ws-switch" title="Switch workspace group" @click="emit('workspaces')">
      <span class="ws-dot">W</span>
      <span class="ws-name">{{ store.config?.workspaces?.[0]?.split('/').pop() || 'No workspace' }}</span>
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

      <router-link to="/explorer" class="sb-nav-item" :class="{ active: route.path === '/explorer' }">
        <span class="sb-icon">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="5" cy="6" r="2"></circle>
            <circle cx="19" cy="6" r="2"></circle>
            <circle cx="12" cy="18" r="2"></circle>
            <line x1="5" y1="8" x2="12" y2="16"></line>
            <line x1="19" y1="8" x2="12" y2="16"></line>
          </svg>
        </span>
        <span>Explorer</span>
      </router-link>

      <router-link to="/office" class="sb-nav-item" :class="{ active: route.path === '/office' }">
        <span class="sb-icon">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
            <rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect>
          </svg>
        </span>
        <span>Office</span>
      </router-link>
    </div>

    <!-- Inspect section -->
    <div class="sb-section">Inspect</div>
    <div class="sb-nav">
      <router-link to="/terminal" class="sb-nav-item" :class="{ active: route.path === '/terminal' }">
        <span class="sb-icon">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="4 17 10 11 4 5"></polyline>
            <line x1="12" y1="19" x2="20" y2="19"></line>
          </svg>
        </span>
        <span>Terminal</span>
        <span class="kbd">&#x2303;`</span>
      </router-link>

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

    <!-- Bottom nav: Docs, Settings, Theme, Workspaces, Containers -->
    <div class="sb-divider"></div>
    <div class="sb-nav">
      <button type="button" class="sb-nav-item" @click="emit('workspaces')">
        <span class="sb-icon">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
            <rect x="2" y="2" width="8" height="8" rx="1"></rect>
            <rect x="14" y="2" width="8" height="8" rx="1"></rect>
            <rect x="2" y="14" width="8" height="8" rx="1"></rect>
            <rect x="14" y="14" width="8" height="8" rx="1"></rect>
          </svg>
        </span>
        <span>Workspaces</span>
      </button>

      <button type="button" class="sb-nav-item" @click="emit('containers')">
        <span class="sb-icon">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
            <rect x="2" y="6" width="20" height="12" rx="2"></rect>
            <line x1="6" y1="10" x2="6" y2="14"></line>
            <line x1="10" y1="10" x2="10" y2="14"></line>
          </svg>
        </span>
        <span>Containers</span>
      </button>

      <button type="button" class="sb-nav-item" @click="emit('settings')" title="Settings">
        <span class="sb-icon">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="12" cy="12" r="3"></circle>
            <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"></path>
          </svg>
        </span>
        <span>Settings</span>
      </button>

      <button type="button" class="sb-nav-item" :title="'Theme: ' + theme" @click="cycle">
        <span class="sb-icon">
          {{ theme === 'light' ? '☀' : theme === 'dark' ? '☾' : '◐' }}
        </span>
        <span>{{ themeLabel() }}</span>
      </button>
    </div>
  </aside>
</template>
