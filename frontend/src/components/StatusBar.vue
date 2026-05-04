<script setup lang="ts">
import { ref, onMounted, computed } from 'vue';
import { useTaskStore } from '../stores/tasks';
import { useUiStore } from '../stores/ui';
import { api } from '../api/client';

const props = defineProps<{ connected: boolean }>();

interface GitWorkspace {
  path: string;
  name?: string;
  branch: string;
  ahead_count?: number;
  behind_count?: number;
  has_remote?: boolean;
  main_branch?: string;
}

const store = useTaskStore();
const ui = useUiStore();
const workspaces = ref<GitWorkspace[]>([]);
const pushing = ref<Record<string, boolean>>({});

const connDotClass = computed(() =>
  props.connected ? 'status-bar-conn-dot--ok' : 'status-bar-conn-dot--closed',
);
const connLabel = computed(() => (props.connected ? 'Connected' : 'Disconnected'));
const workspaceLabel = computed(() => {
  const ws = store.config?.workspaces;
  if (!ws || !ws.length) return '';
  return ws.map((w) => w.split('/').pop()).join(', ');
});

async function refreshGitStatus() {
  try {
    const status = await api<{ workspaces: GitWorkspace[] }>('GET', '/api/git/status');
    workspaces.value = status?.workspaces ?? [];
  } catch {
    /* git status optional */
  }
}

async function pushWorkspace(ws: GitWorkspace) {
  if (pushing.value[ws.path]) return;
  pushing.value[ws.path] = true;
  try {
    await api('POST', '/api/git/push', { workspace: ws.path });
    await refreshGitStatus();
  } catch {
    /* surface failures via status refresh; no modal in Vue UI yet */
  } finally {
    pushing.value[ws.path] = false;
  }
}

onMounted(() => {
  refreshGitStatus();
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
        v-if="workspaces.length"
        class="status-bar-branches"
        aria-label="Workspace branches"
      >
        <span
          v-for="ws in workspaces"
          :key="ws.path"
          class="status-bar-branch-group"
        >
          <span class="status-bar-branch" :title="`${ws.path || ws.name || ''}\nBranch: ${ws.branch}`">
            <span class="status-bar-branch__glyph">⎇</span>
            <span class="status-bar-branch__name">{{ ws.branch }}</span>
          </span>
          <template v-if="ws.has_remote">
            <span
              v-if="(ws.behind_count ?? 0) > 0"
              class="status-bar-branch__badge status-bar-branch__badge--behind"
              :title="`${ws.behind_count} commits behind upstream`"
            >{{ ws.behind_count }}↓</span>
            <span
              v-if="(ws.ahead_count ?? 0) > 0"
              class="status-bar-branch__badge status-bar-branch__badge--ahead"
              :title="`${ws.ahead_count} commits ahead of upstream`"
            >{{ ws.ahead_count }}↑</span>
            <button
              v-if="(ws.ahead_count ?? 0) > 0"
              type="button"
              class="status-bar-branch__action status-bar-branch__action--push"
              :disabled="pushing[ws.path]"
              :title="`Push ${ws.ahead_count} commits to upstream`"
              @click="pushWorkspace(ws)"
            >{{ pushing[ws.path] ? '...' : 'Push' }}</button>
          </template>
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
      <button
        type="button"
        class="status-bar-btn"
        title="Toggle terminal panel (Ctrl+`)"
        :aria-expanded="ui.showTerminal"
        aria-controls="status-bar-panel"
        @click="ui.toggleTerminal()"
      >
        Terminal <kbd class="status-bar-kbd">^`</kbd>
      </button>
    </div>
  </footer>
</template>
