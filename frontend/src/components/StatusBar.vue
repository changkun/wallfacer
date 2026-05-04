<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue';
import { useTaskStore } from '../stores/tasks';
import { useUiStore } from '../stores/ui';
import { api } from '../api/client';

const props = defineProps<{ connected: boolean }>();
defineEmits<{ shortcuts: [] }>();

interface GitWorkspace {
  path: string;
  name?: string;
  branch: string;
  is_git_repo?: boolean;
  ahead_count?: number;
  behind_count?: number;
  has_remote?: boolean;
  main_branch?: string;
  behind_main_count?: number;
}

const store = useTaskStore();
const ui = useUiStore();
const workspaces = ref<GitWorkspace[]>([]);
const busy = ref<Record<string, string>>({}); // ws.path -> 'push' | 'sync' | 'rebase'

const connDotClass = computed(() =>
  props.connected ? 'status-bar-conn-dot--ok' : 'status-bar-conn-dot--closed',
);
const connLabel = computed(() => (props.connected ? 'Connected' : 'Disconnected'));

const workspaceLabel = computed(() => {
  const ws = store.config?.workspaces ?? [];
  if (!ws.length) return '';
  const groups = store.config?.workspace_groups ?? [];
  const matched = groups.find(g =>
    Array.isArray(g.workspaces)
      && g.workspaces.length === ws.length
      && g.workspaces.every((p, i) => p === ws[i]),
  );
  if (matched?.name) return matched.name;
  const first = ws[0].replace(/\/+$/, '').split('/');
  return first[first.length - 1] || ws[0];
});

const renderableWorkspaces = computed(() =>
  workspaces.value.filter(w => (w.is_git_repo ?? true) && w.branch),
);
const isMulti = computed(() => renderableWorkspaces.value.length > 1);

function branchLabel(ws: GitWorkspace): string {
  return isMulti.value && ws.name ? `${ws.name}:${ws.branch}` : ws.branch;
}

async function refreshGitStatus() {
  try {
    const status = await api<{ workspaces: GitWorkspace[] }>('GET', '/api/git/status');
    workspaces.value = status?.workspaces ?? [];
  } catch {
    /* git status optional */
  }
}

async function runAction(ws: GitWorkspace, kind: 'push' | 'sync' | 'rebase') {
  if (busy.value[ws.path]) return;
  busy.value[ws.path] = kind;
  try {
    const route = kind === 'push'
      ? '/api/git/push'
      : kind === 'sync'
        ? '/api/git/sync'
        : '/api/git/rebase-on-main';
    await api('POST', route, { workspace: ws.path });
    await refreshGitStatus();
  } catch {
    /* error surfaced via stale state; status SSE will re-emit */
  } finally {
    delete busy.value[ws.path];
  }
}

let sse: EventSource | null = null;
function startGitStream() {
  if (typeof EventSource === 'undefined') return;
  try {
    let url = '/api/git/stream';
    const key = window.__WALLFACER__?.serverApiKey;
    if (key) url += `?token=${encodeURIComponent(key)}`;
    sse = new EventSource(url);
    sse.addEventListener('git-status', (ev) => {
      try {
        const msg = JSON.parse((ev as MessageEvent<string>).data) as { workspaces?: GitWorkspace[] };
        if (Array.isArray(msg.workspaces)) workspaces.value = msg.workspaces;
      } catch { /* ignore */ }
    });
  } catch {
    /* sse optional */
  }
}

onMounted(() => {
  refreshGitStatus();
  startGitStream();
});

onUnmounted(() => {
  sse?.close();
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
        v-if="renderableWorkspaces.length"
        class="status-bar-branches"
        aria-label="Workspace branches"
      >
        <span
          v-for="ws in renderableWorkspaces"
          :key="ws.path"
          class="status-bar-branch-group"
        >
          <button
            type="button"
            class="status-bar-branch"
            :title="`${ws.path || ws.name || ''}\nBranch: ${ws.branch}`"
          >
            <span class="status-bar-branch__glyph">⎇</span>
            <span class="status-bar-branch__name">{{ branchLabel(ws) }}</span>
          </button>
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
              v-if="(ws.behind_count ?? 0) > 0"
              type="button"
              class="status-bar-branch__action status-bar-branch__action--sync"
              :disabled="!!busy[ws.path]"
              :title="`Pull ${ws.behind_count} commits from upstream`"
              @click="runAction(ws, 'sync')"
            >{{ busy[ws.path] === 'sync' ? '…' : 'Sync' }}</button>
            <button
              v-if="(ws.ahead_count ?? 0) > 0"
              type="button"
              class="status-bar-branch__action status-bar-branch__action--push"
              :disabled="!!busy[ws.path]"
              :title="`Push ${ws.ahead_count} commits to upstream`"
              @click="runAction(ws, 'push')"
            >{{ busy[ws.path] === 'push' ? '…' : 'Push' }}</button>
            <button
              v-if="ws.main_branch && ws.branch !== ws.main_branch"
              type="button"
              class="status-bar-branch__action status-bar-branch__action--rebase"
              :disabled="!!busy[ws.path]"
              :title="`Fetch origin/${ws.main_branch} and rebase current branch on top`"
              @click="runAction(ws, 'rebase')"
            ><template v-if="(ws.behind_main_count ?? 0) > 0">{{ ws.behind_main_count }}↓ </template>{{ busy[ws.path] === 'rebase' ? '…' : `Rebase on ${ws.main_branch}` }}</button>
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
      <router-link to="/map" class="status-bar-btn" title="Open map">Map</router-link>
      <button
        type="button"
        class="status-bar-btn"
        title="Keyboard shortcuts"
        aria-label="Show keyboard shortcuts"
        @click="$emit('shortcuts')"
      >
        Shortcuts <kbd class="status-bar-kbd">?</kbd>
      </button>
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
