<script setup lang="ts">
import { useTaskStore } from '../stores/tasks';

defineProps<{ connected: boolean }>();

const store = useTaskStore();
</script>

<template>
  <footer class="status-bar">
    <span class="sb-dot" :class="connected ? 'sb-ok' : 'sb-off'" />
    <span class="sb-text">{{ connected ? 'Connected' : 'Disconnected' }}</span>
    <span v-if="store.config?.workspaces?.length" class="sb-sep" />
    <span v-if="store.config?.workspaces?.length" class="sb-text sb-workspace">
      {{ store.config.workspaces.map(w => w.split('/').pop()).join(', ') }}
    </span>
    <span class="sb-spacer" />
    <span class="sb-text sb-count" title="Waiting / Failed">
      ◎ {{ store.waiting.length }}
    </span>
    <span class="sb-sep" />
    <span class="sb-text sb-count" title="In Progress">
      ● {{ store.inProgress.length }}
    </span>
  </footer>
</template>

<style scoped>
.status-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 24px;
  padding: 0 10px;
  background: var(--bg-sunk);
  border-top: 1px solid var(--rule);
  font-size: 11px;
  color: var(--ink-3);
  flex-shrink: 0;
}
.sb-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}
.sb-ok { background: var(--ok); }
.sb-off { background: var(--err); }
.sb-text { white-space: nowrap; }
.sb-workspace { font-family: var(--font-mono); font-size: 10px; }
.sb-sep {
  width: 1px;
  height: 10px;
  background: var(--rule);
}
.sb-spacer { flex: 1; }
.sb-count { font-family: var(--font-mono); font-size: 10px; }
</style>
