<script setup lang="ts">
import { ref, onMounted, computed } from 'vue';
import { useTaskStore } from '../stores/tasks';
import { api } from '../api/client';

const props = defineProps<{ connected: boolean }>();

const store = useTaskStore();
const gitBranch = ref('');

const connDotClass = computed(() =>
  props.connected ? 'status-bar-conn-dot--ok' : 'status-bar-conn-dot--closed',
);
const connLabel = computed(() => (props.connected ? 'Connected' : 'Disconnected'));
const workspaceLabel = computed(() => {
  const ws = store.config?.workspaces;
  if (!ws || !ws.length) return '';
  return ws.map((w) => w.split('/').pop()).join(', ');
});

onMounted(async () => {
  try {
    const status = await api<{ workspaces: { branch: string }[] }>('GET', '/api/git/status');
    if (status?.workspaces?.length) {
      gitBranch.value = status.workspaces[0].branch || '';
    }
  } catch {
    /* git status optional */
  }
});
</script>

<template>
  <footer class="status-bar" role="contentinfo" aria-label="Status bar">
    <div class="status-bar__left">
      <span
        class="status-bar-conn-dot"
        :class="connDotClass"
        :aria-label="connLabel"
      />
      <span class="status-bar-conn-label">{{ connLabel }}</span>
      <span v-if="workspaceLabel" class="status-bar-workspace">{{ workspaceLabel }}</span>
      <span
        v-if="gitBranch"
        class="status-bar-branches"
        aria-label="Workspace branches"
      >
        <span class="status-bar-branch-group">
          <span class="status-bar-branch">
            <span class="status-bar-branch__glyph">⑂</span>
            <span class="status-bar-branch__name">{{ gitBranch }}</span>
          </span>
        </span>
      </span>
    </div>
    <div class="status-bar__center">
      <span class="status-bar-count" title="In Progress">
        <svg width="10" height="10" viewBox="0 0 10 10" fill="none" aria-hidden="true">
          <circle cx="5" cy="5" r="4" stroke="currentColor" stroke-width="1.5" />
          <polyline
            points="5,2.5 5,5 7,6.5"
            stroke="currentColor"
            stroke-width="1.2"
            stroke-linecap="round"
          />
        </svg>
        <span id="status-bar-in-progress">{{ store.inProgress.length }}</span>
      </span>
      <span class="status-bar-count" title="Waiting">
        <svg width="10" height="10" viewBox="0 0 10 10" fill="none" aria-hidden="true">
          <circle cx="5" cy="5" r="4" stroke="currentColor" stroke-width="1.5" />
          <line
            x1="5"
            y1="3"
            x2="5"
            y2="5.5"
            stroke="currentColor"
            stroke-width="1.2"
            stroke-linecap="round"
          />
          <circle cx="5" cy="7" r="0.6" fill="currentColor" />
        </svg>
        <span id="status-bar-waiting">{{ store.waiting.length }}</span>
      </span>
    </div>
    <div class="status-bar__right">
      <router-link to="/office" class="status-bar-btn" title="Open office">
        Office
      </router-link>
      <router-link to="/terminal" class="status-bar-btn" title="Open terminal">
        Terminal
      </router-link>
    </div>
  </footer>
</template>
