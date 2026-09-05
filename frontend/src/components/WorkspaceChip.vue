<script setup lang="ts">
// The workspace block at the top of the rail: the switcher (name, caret,
// popover of groups) with the live-connection state as a dot before the name,
// and one branch row per git workspace with ahead/behind counts and the
// sync / push / rebase actions. This is where the status bar's contents
// re-homed (specs/shared/console-redesign/shell.md): connection and branches
// are workspace state, so they live with the workspace.
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { useTaskStore } from '../stores/tasks';
import { useUiStore } from '../stores/ui';
import { useWorkspacesStore } from '../stores/workspaces';
import { useDialogStore } from '../stores/dialog';
import { useToastStore } from '../stores/toast';
import { api, ApiError } from '../api/client';
import { useSse } from '../composables/useSse';
import { formatGitConflict } from '../lib/gitConflict';
import { groupLabel, workspaceLabel } from '../lib/workspaceLabel';
import BranchDropdown from './BranchDropdown.vue';

interface WorkspaceGroup {
  // id ties the saved group back to its registry workspace; group actions route
  // through the id-based registry endpoints.
  id?: string;
  name?: string;
  workspaces: string[];
  key?: string;
}

export interface GitWorkspace {
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

const props = defineProps<{
  collapsed: boolean;
  connState: 'ok' | 'reconnecting' | 'closed';
}>();
const emit = defineEmits<{ workspaces: [] }>();

const store = useTaskStore();
const ui = useUiStore();
const wsStore = useWorkspacesStore();
const dialog = useDialogStore();
const toast = useToastStore();

// ---- switcher -------------------------------------------------------------
const popoverOpen = ref(false);
const switchingKey = ref('');
const workspaceGroups = computed<WorkspaceGroup[]>(() => store.config?.workspace_groups ?? []);

const activeLabel = computed(() => {
  const active = wsStore.active;
  if (active) return workspaceLabel(active.name, active.folders);
  const ws = store.config?.workspaces;
  if (!ws || ws.length === 0) return 'No workspace';
  const groups = store.config?.workspace_groups ?? [];
  const key = JSON.stringify(ws);
  const matched = groups.find((g) => JSON.stringify(g.workspaces) === key);
  return workspaceLabel(matched?.name, ws);
});

// Per-group running/waiting badge. The active group reads the live task list;
// background groups use the server's active_groups info.
function groupBadge(g: WorkspaceGroup): { inProgress: number; waiting: number } {
  if (isActiveGroup(g)) return { inProgress: store.inProgress.length, waiting: store.waiting.length };
  const info = (store.config?.active_groups ?? []).find((a) => a.key === g.key);
  return { inProgress: info?.in_progress ?? 0, waiting: info?.waiting ?? 0 };
}
function activeKey(): string {
  return JSON.stringify(store.config?.workspaces ?? []);
}
function isActiveGroup(g: WorkspaceGroup): boolean {
  if (g.id) return wsStore.isActive(g.id);
  return JSON.stringify(g.workspaces) === activeKey();
}
async function switchToGroup(g: WorkspaceGroup) {
  if (isActiveGroup(g) || switchingKey.value) return;
  if (!g.id) return; // pre-migration group without an id: skip rather than crash
  switchingKey.value = g.key ?? JSON.stringify(g.workspaces);
  try {
    await wsStore.activate(g.id);
    toast.push(`Switched to ${g.name || 'workspace'}`, { kind: 'success' });
    popoverOpen.value = false;
  } catch (e) {
    toast.push(`Switch failed: ${e instanceof Error ? e.message : String(e)}`, { kind: 'error' });
  } finally {
    switchingKey.value = '';
  }
}
function isSwitching(g: WorkspaceGroup): boolean {
  return switchingKey.value === (g.key ?? JSON.stringify(g.workspaces));
}
function editGroup(g: WorkspaceGroup) {
  if (!g.id) return;
  popoverOpen.value = false;
  ui.openWorkspaceEdit(g.id);
}
async function deleteGroup(g: WorkspaceGroup) {
  if (!g.id) return;
  const ok = await dialog.confirm({
    title: 'Delete workspace',
    message: `Remove the ${g.name || 'unnamed'} workspace? Folders on disk are not touched, but the workspace will no longer be reachable until recreated.`,
    confirmLabel: 'Delete',
    danger: true,
  });
  if (!ok) return;
  try {
    await wsStore.remove(g.id);
    await store.fetchConfig();
    toast.push('Deleted workspace', { kind: 'success' });
  } catch (e) {
    toast.push(`Delete failed: ${e instanceof Error ? e.message : String(e)}`, { kind: 'error' });
  }
}

let outsideHandler: ((e: MouseEvent) => void) | null = null;
function removeOutsideHandler() {
  if (outsideHandler) {
    document.removeEventListener('mousedown', outsideHandler);
    outsideHandler = null;
  }
}
watch(popoverOpen, (open) => {
  removeOutsideHandler();
  if (!open) return;
  const handler = (e: MouseEvent) => {
    if (!(e.target as HTMLElement).closest('.sb-ws-switch-wrap')) popoverOpen.value = false;
  };
  outsideHandler = handler;
  setTimeout(() => {
    if (outsideHandler === handler) document.addEventListener('mousedown', handler);
  }, 0);
});
onUnmounted(removeOutsideHandler);

// ---- connection -------------------------------------------------------------
const connLabel = computed(() =>
  props.connState === 'ok' ? 'Connected' : props.connState === 'reconnecting' ? 'Reconnecting…' : 'Disconnected',
);

// ---- branches ---------------------------------------------------------------
const workspaces = ref<GitWorkspace[]>([]);
const busy = ref<Record<string, string>>({}); // ws.path -> 'push' | 'sync' | 'rebase'
const renderable = computed(() => workspaces.value.filter((w) => (w.is_git_repo ?? true) && w.branch));
const isMulti = computed(() => renderable.value.length > 1);
function branchLabel(ws: GitWorkspace): string {
  return isMulti.value && ws.name ? `${ws.name}:${ws.branch}` : ws.branch;
}

const dropdownOpen = ref(false);
const dropdownWs = ref<GitWorkspace | null>(null);
const dropdownAnchor = ref<{ top: number; left: number } | null>(null);
function openBranchDropdown(ws: GitWorkspace, e: MouseEvent) {
  e.stopPropagation();
  if (dropdownOpen.value && dropdownWs.value?.path === ws.path) {
    dropdownOpen.value = false;
    return;
  }
  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
  dropdownAnchor.value = { top: rect.top, left: rect.right + 6 };
  dropdownWs.value = ws;
  dropdownOpen.value = true;
}

async function refreshGitStatus() {
  try {
    const list = await api<GitWorkspace[]>('GET', '/api/git/status');
    workspaces.value = Array.isArray(list) ? list : [];
  } catch {
    /* git status optional */
  }
}

async function runAction(ws: GitWorkspace, kind: 'push' | 'sync' | 'rebase') {
  if (busy.value[ws.path]) return;
  busy.value[ws.path] = kind;
  try {
    const route = kind === 'push' ? '/api/git/push' : kind === 'sync' ? '/api/git/sync' : '/api/git/rebase-on-main';
    await api('POST', route, { workspace: ws.path });
    await refreshGitStatus();
  } catch (e) {
    if (e instanceof ApiError && e.status === 409) {
      const label = kind.charAt(0).toUpperCase() + kind.slice(1);
      toast.push(formatGitConflict(e.body, label), { kind: 'error' });
    }
    /* other errors surface via stale state; status SSE will re-emit */
  } finally {
    delete busy.value[ws.path];
  }
}

useSse({
  url: '/api/git/stream',
  withCredentials: false,
  onMessage: (ev) => {
    try {
      const list = typeof ev.data === 'string' ? JSON.parse(ev.data) : ev.data;
      if (Array.isArray(list)) workspaces.value = list as GitWorkspace[];
    } catch { /* ignore */ }
  },
});
onMounted(() => { void refreshGitStatus(); });

defineExpose({ runAction, refreshGitStatus, workspaces });
</script>

<template>
  <div class="ws-chip" :class="{ 'ws-chip--fold': collapsed }">
    <div class="sb-ws-switch-wrap" :class="{ 'sb-ws-switch-wrap--collapsed': collapsed }">
      <button
        type="button"
        class="sb-ws-switch"
        :class="{ 'sb-ws-switch--icon': collapsed }"
        :title="`${activeLabel} · ${connLabel}`"
        :aria-expanded="popoverOpen"
        @click="popoverOpen = !popoverOpen"
      >
        <span class="ws-dot">W</span>
        <template v-if="!collapsed">
          <span class="ws-name">{{ activeLabel }}</span>
          <span class="ws-conn" :class="'ws-conn--' + connState" :aria-label="connLabel" :data-conn="connState" />
          <span class="ws-caret" aria-hidden="true">
            <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"></polyline></svg>
          </span>
        </template>
        <span v-else class="ws-conn ws-conn--fold" :class="'ws-conn--' + connState" :aria-label="connLabel" :data-conn="connState" />
      </button>
      <div v-if="popoverOpen" class="sb-ws-popover sb-ws-popover--inline" role="menu" @click.stop>
        <button
          v-for="g in workspaceGroups"
          :key="g.key ?? (g.workspaces ?? []).join('|')"
          type="button"
          class="sb-ws-popover__item"
          :class="{ active: isActiveGroup(g) }"
          role="menuitem"
          :title="(g.workspaces ?? []).join(', ')"
          @click="switchToGroup(g)"
        >
          <span class="sb-ws-popover__check">{{ isActiveGroup(g) ? '✓' : '' }}</span>
          <span class="sb-ws-popover__label">{{ groupLabel(g) }}</span>
          <span v-if="isSwitching(g)" class="sb-ws-popover__switching muted">switching…</span>
          <span v-else class="sb-ws-popover__counts">
            <span v-if="groupBadge(g).inProgress > 0" class="pill pill-run" :title="`${groupBadge(g).inProgress} running`">{{ groupBadge(g).inProgress }}</span>
            <span v-if="groupBadge(g).waiting > 0" class="pill pill-warn" :title="`${groupBadge(g).waiting} waiting`">{{ groupBadge(g).waiting }}</span>
          </span>
          <span class="sb-ws-popover__row-actions">
            <button type="button" class="sb-ws-popover__row-btn sb-ws-popover__row-btn--edit" :title="`Edit ${g.name || 'workspace'}`" @click.stop="editGroup(g)">Edit</button>
            <button type="button" class="sb-ws-popover__row-btn" :title="`Delete ${g.name || 'workspace'}`" @click.stop="deleteGroup(g)">×</button>
          </span>
        </button>
        <div class="sb-ws-popover__divider" />
        <button type="button" class="sb-ws-popover__item sb-ws-popover__add" role="menuitem" @click="popoverOpen = false; emit('workspaces')">
          <span class="sb-ws-popover__check">+</span>
          <span class="sb-ws-popover__label">Add workspace…</span>
        </button>
      </div>
    </div>

    <div v-if="!collapsed && renderable.length" class="ws-branches" aria-label="Workspace branches">
      <div v-for="ws in renderable" :key="ws.path" class="ws-branch-row">
        <button
          type="button"
          class="ws-branch"
          :title="`${ws.path || ws.name || ''}\nBranch: ${ws.branch}`"
          @click="openBranchDropdown(ws, $event)"
        >
          <span class="ws-branch__glyph" aria-hidden="true">⎇</span>
          <span class="ws-branch__name">{{ branchLabel(ws) }}</span>
          <template v-if="ws.has_remote">
            <span v-if="(ws.behind_count ?? 0) > 0" class="ws-branch__count ws-branch__count--behind" :title="`${ws.behind_count} commits behind upstream`">{{ ws.behind_count }}↓</span>
            <span v-if="(ws.ahead_count ?? 0) > 0" class="ws-branch__count ws-branch__count--ahead" :title="`${ws.ahead_count} commits ahead of upstream`">{{ ws.ahead_count }}↑</span>
          </template>
        </button>
        <div v-if="ws.has_remote && ((ws.behind_count ?? 0) > 0 || (ws.ahead_count ?? 0) > 0 || (ws.main_branch && ws.branch !== ws.main_branch))" class="ws-branch-actions">
          <button
            v-if="(ws.behind_count ?? 0) > 0"
            type="button"
            class="btn sm ghost ws-branch-action"
            :class="{ pending: busy[ws.path] === 'sync' }"
            :disabled="!!busy[ws.path]"
            :title="`Pull ${ws.behind_count} commits from upstream`"
            @click="runAction(ws, 'sync')"
          >Sync</button>
          <button
            v-if="(ws.ahead_count ?? 0) > 0"
            type="button"
            class="btn sm ghost ws-branch-action"
            :class="{ pending: busy[ws.path] === 'push' }"
            :disabled="!!busy[ws.path]"
            :title="`Push ${ws.ahead_count} commits to upstream`"
            @click="runAction(ws, 'push')"
          >Push</button>
          <button
            v-if="ws.main_branch && ws.branch !== ws.main_branch"
            type="button"
            class="btn sm ghost ws-branch-action"
            :class="{ pending: busy[ws.path] === 'rebase' }"
            :disabled="!!busy[ws.path]"
            :title="`Fetch origin/${ws.main_branch} and rebase current branch on top`"
            @click="runAction(ws, 'rebase')"
          ><template v-if="(ws.behind_main_count ?? 0) > 0">{{ ws.behind_main_count }}↓ </template>Rebase</button>
        </div>
      </div>
    </div>

    <BranchDropdown
      v-if="dropdownWs"
      v-model="dropdownOpen"
      :workspace-path="dropdownWs.path"
      :current-branch="dropdownWs.branch"
      :anchor="dropdownAnchor"
      @switched="refreshGitStatus"
    />
  </div>
</template>
