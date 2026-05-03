<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { useTaskStore } from '../stores/tasks';
import { api } from '../api/client';

defineProps<{ connected: boolean }>();

const store = useTaskStore();
const gitBranch = ref('');

onMounted(async () => {
  try {
    const status = await api<{ workspaces: { branch: string }[] }>('GET', '/api/git/status');
    if (status?.workspaces?.length) {
      gitBranch.value = status.workspaces[0].branch || '';
    }
  } catch { /* git status optional */ }
});
</script>

<template>
  <footer class="status-bar">
    <span class="sb-dot" :class="connected ? 'sb-ok' : 'sb-off'" />
    <span class="sb-text">{{ connected ? 'Connected' : 'Disconnected' }}</span>
    <span v-if="store.config?.workspaces?.length" class="sb-sep" />
    <span v-if="store.config?.workspaces?.length" class="sb-text sb-workspace">
      {{ store.config.workspaces.map(w => w.split('/').pop()).join(', ') }}
    </span>
    <span v-if="gitBranch" class="sb-sep" />
    <span v-if="gitBranch" class="sb-text sb-branch">⑂ {{ gitBranch }}</span>
    <span class="sb-spacer" />
    <span class="sb-text sb-count" title="Waiting / Failed">◎ {{ store.waiting.length }}</span>
    <span class="sb-sep" />
    <span class="sb-text sb-count" title="In Progress">● {{ store.inProgress.length }}</span>
    <span class="sb-sep" />
    <router-link to="/office" class="sb-link">Office</router-link>
    <span class="sb-sep" />
    <router-link to="/terminal" class="sb-link">Terminal</router-link>
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
.sb-dot { width: 6px; height: 6px; border-radius: 50%; flex-shrink: 0; }
.sb-ok { background: var(--ok); }
.sb-off { background: var(--err); }
.sb-text { white-space: nowrap; }
.sb-workspace { font-family: var(--font-mono); font-size: 10px; }
.sb-branch { font-family: var(--font-mono); font-size: 10px; }
.sb-sep { width: 1px; height: 10px; background: var(--rule); }
.sb-spacer { flex: 1; }
.sb-count { font-family: var(--font-mono); font-size: 10px; }
.sb-link { font-size: 10px; color: var(--ink-3); text-decoration: none; cursor: pointer; }
.sb-link:hover { color: var(--ink); }
</style>
