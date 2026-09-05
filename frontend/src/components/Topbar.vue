<script setup lang="ts">
// The one bar every page shares: a crumb on the left (workspace › page › leaf)
// and, on the right, the page's own actions (a component the page registers
// on the ui store) followed by the two console-wide controls, terminal and
// keyboard shortcuts. Below the phone breakpoint a menu button opens the rail
// drawer. See specs/shared/console-redesign/shell.md.
import { computed } from 'vue';
import { useRoute } from 'vue-router';
import { useTaskStore } from '../stores/tasks';
import { useUiStore } from '../stores/ui';
import { useWorkspacesStore } from '../stores/workspaces';
import { workspaceLabel } from '../lib/workspaceLabel';
import { navLabel } from '../lib/nav';

const route = useRoute();
const store = useTaskStore();
const ui = useUiStore();
const wsStore = useWorkspacesStore();

const workspaceName = computed(() => {
  const active = wsStore.active;
  if (active) return workspaceLabel(active.name, active.folders);
  const ws = store.config?.workspaces;
  if (!ws || ws.length === 0) return '';
  return workspaceLabel(undefined, ws);
});
const pageName = computed(() => navLabel(route.path));
const crumb = computed(() => [workspaceName.value, pageName.value, ui.crumbLeaf].filter(Boolean));
</script>

<template>
  <header class="topbar">
    <button
      type="button"
      class="icon-btn topbar-menu"
      title="Open navigation"
      aria-label="Open navigation"
      :aria-expanded="ui.railOpen"
      @click="ui.toggleRail()"
    >
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true">
        <line x1="4" y1="7" x2="20" y2="7"></line><line x1="4" y1="12" x2="20" y2="12"></line><line x1="4" y1="17" x2="20" y2="17"></line>
      </svg>
    </button>
    <nav class="crumb" aria-label="Breadcrumb">
      <template v-for="(part, i) in crumb" :key="i">
        <span v-if="i > 0" class="sep" aria-hidden="true">›</span>
        <span :class="i === crumb.length - 1 ? 'leaf' : ''">{{ part }}</span>
      </template>
    </nav>
    <div class="topbar-actions">
      <div v-if="ui.topbarActions" id="topbar-actions" class="topbar-page-actions">
        <component :is="ui.topbarActions" />
      </div>
      <button
        type="button"
        class="icon-btn"
        :class="{ on: ui.showTerminal }"
        title="Toggle terminal (Ctrl+`)"
        aria-label="Toggle terminal"
        :aria-pressed="ui.showTerminal"
        data-action="terminal"
        @click="ui.toggleTerminal()"
      >
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <polyline points="4 17 10 11 4 5"></polyline><line x1="12" y1="19" x2="20" y2="19"></line>
        </svg>
      </button>
      <button
        type="button"
        class="icon-btn"
        title="Keyboard shortcuts (?)"
        aria-label="Keyboard shortcuts"
        data-action="shortcuts"
        @click="ui.openShortcuts()"
      >
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
          <rect x="2" y="6" width="20" height="12" rx="2"></rect><line x1="6" y1="10" x2="6" y2="10"></line><line x1="10" y1="10" x2="10" y2="10"></line><line x1="14" y1="10" x2="14" y2="10"></line><line x1="18" y1="10" x2="18" y2="10"></line><line x1="8" y1="14" x2="16" y2="14"></line>
        </svg>
      </button>
    </div>
  </header>
</template>
