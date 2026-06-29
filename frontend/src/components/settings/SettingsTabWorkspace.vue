<script setup lang="ts">
import { ref, computed, onMounted, reactive } from 'vue';
import { api } from '../../api/client';
import { useTaskStore } from '../../stores/tasks';
import { useWorkspacesStore } from '../../stores/workspaces';
import { useDialogStore } from '../../stores/dialog';
import type { Workspace, WorkspaceGroup } from '../../api/types';

const emit = defineEmits<{ workspaces: [] }>();
const store = useTaskStore();
const wsStore = useWorkspacesStore();
const dialog = useDialogStore();
const status = ref('');
const switchingKey = ref<string | null>(null);

// busyId tracks the workspace currently mid-mutation so its row controls
// disable without freezing the whole list.
const busyId = ref<string | null>(null);
// nameDrafts holds the in-progress rename text per workspace id, seeded lazily
// from the registry so an unsaved edit is not clobbered by a refresh.
const nameDrafts = reactive<Record<string, string>>({});

const workspaces = computed<Workspace[]>(() => wsStore.workspaces);
const activeWorkspaces = computed(() => store.config?.workspaces ?? []);
const workspaceGroups = computed<WorkspaceGroup[]>(() => store.config?.workspace_groups ?? []);

onMounted(() => {
  if (wsStore.workspaces.length === 0) void wsStore.list();
});

function nameDraft(ws: Workspace): string {
  return nameDrafts[ws.id] ?? ws.name ?? '';
}

function basename(p: string): string {
  const clean = String(p || '').replace(/[\\/]+$/, '');
  const parts = clean.split(/[\\/]/);
  return parts[parts.length - 1] || clean;
}

function setStatus(msg: string) {
  status.value = msg;
  if (msg === 'Saved.') setTimeout(() => { if (status.value === 'Saved.') status.value = ''; }, 2000);
}

// Rename writes the draft name through PUT /api/workspaces/{id}; folders are
// untouched so history is preserved.
async function rename(ws: Workspace) {
  const next = nameDraft(ws).trim();
  if (!next || next === ws.name || busyId.value) return;
  busyId.value = ws.id;
  status.value = '';
  try {
    await wsStore.update(ws.id, { name: next });
    delete nameDrafts[ws.id];
    setStatus('Saved.');
  } catch (e) {
    setStatus('Error: ' + (e instanceof Error ? e.message : String(e)));
  } finally {
    busyId.value = null;
  }
}

// removeFolder drops a single folder via PUT folders. The server rejects an
// empty folder set (400), so the last folder cannot be removed here — that is
// what "Re-point folders" (the picker browser) is for.
async function removeFolder(ws: Workspace, path: string) {
  if (busyId.value || ws.folders.length <= 1) return;
  busyId.value = ws.id;
  status.value = '';
  try {
    await wsStore.update(ws.id, { folders: ws.folders.filter(f => f !== path) });
    setStatus('Saved.');
  } catch (e) {
    setStatus('Error: ' + (e instanceof Error ? e.message : String(e)));
  } finally {
    busyId.value = null;
  }
}

async function activate(ws: Workspace) {
  if (busyId.value || wsStore.isActive(ws.id)) return;
  busyId.value = ws.id;
  status.value = '';
  try {
    await wsStore.activate(ws.id);
    setStatus('Switched.');
  } catch (e) {
    setStatus('Error: ' + (e instanceof Error ? e.message : String(e)));
  } finally {
    busyId.value = null;
  }
}

// remove deletes a workspace. The active one is guarded server-side (409) and
// disabled in the UI; switch away first.
async function remove(ws: Workspace) {
  if (busyId.value || wsStore.isActive(ws.id)) return;
  const ok = await dialog.confirm({
    title: 'Delete workspace',
    message: `Delete "${ws.name || basename(ws.folders[0] ?? '')}"? Folders on disk are not touched.`,
    confirmLabel: 'Delete',
    danger: true,
  });
  if (!ok) return;
  busyId.value = ws.id;
  status.value = '';
  try {
    await wsStore.remove(ws.id);
    setStatus('Deleted.');
  } catch (e) {
    setStatus('Error: ' + (e instanceof Error ? e.message : String(e)));
  } finally {
    busyId.value = null;
  }
}

function workspaceGroupLabel(group: WorkspaceGroup): string {
  if (!group || !Array.isArray(group.workspaces) || !group.workspaces.length) return 'Empty group';
  if (group.name) return group.name;
  const names = group.workspaces.map(p => {
    const clean = String(p || '').replace(/[\\/]+$/, '');
    const parts = clean.split(/[\\/]/);
    return parts[parts.length - 1] || clean;
  });
  return names.join(' + ');
}

function isActiveGroup(group: WorkspaceGroup): boolean {
  const a = group.workspaces;
  const b = activeWorkspaces.value;
  if (!Array.isArray(a) || a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}

async function useGroup(group: WorkspaceGroup) {
  if (switchingKey.value) return;
  switchingKey.value = group.key;
  status.value = '';
  try {
    await api('PUT', '/api/workspaces', { workspaces: group.workspaces });
    await store.fetchConfig();
    await store.fetchTasks();
  } catch (e) {
    status.value = 'Error: ' + (e instanceof Error ? e.message : String(e));
  } finally {
    switchingKey.value = null;
  }
}

// Per-group parallel overrides: writes the whole workspace_groups array
// back to /api/config with the edited group's max_parallel /
// max_test_parallel replaced. 0 / empty input clears the override so the
// global default takes over again.
const savingGroup = ref<string | null>(null);
async function saveGroupLimits(group: WorkspaceGroup, maxParallel: number | null, maxTestParallel: number | null) {
  if (savingGroup.value) return;
  savingGroup.value = group.key;
  status.value = '';
  try {
    const next = workspaceGroups.value.map((g) => {
      if (g.key !== group.key) return { name: g.name, workspaces: g.workspaces, max_parallel: g.max_parallel, max_test_parallel: g.max_test_parallel };
      return {
        name: g.name,
        workspaces: g.workspaces,
        max_parallel: maxParallel && maxParallel > 0 ? maxParallel : undefined,
        max_test_parallel: maxTestParallel && maxTestParallel > 0 ? maxTestParallel : undefined,
      };
    });
    await api('PUT', '/api/config', { workspace_groups: next });
    await store.fetchConfig();
    status.value = 'Saved.';
    setTimeout(() => { if (status.value === 'Saved.') status.value = ''; }, 2000);
  } catch (e) {
    status.value = 'Error: ' + (e instanceof Error ? e.message : String(e));
  } finally {
    savingGroup.value = null;
  }
}
</script>

<template>
  <div class="settings-tab-content active" data-settings-tab="workspace">
    <div class="settings-section">
      <div style="display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 8px;">
        <div style="font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px;">
          Workspaces
        </div>
        <span
          id="settings-workspace-status"
          style="font-size: 11px; color: var(--text-muted)"
        >{{ status }}</span>
      </div>

      <div id="settings-workspace-list" class="ws-editor">
        <div v-if="workspaces.length === 0" style="color: var(--text-muted); font-size: 12px;">
          No workspaces yet. Create one to get started.
        </div>
        <div
          v-for="ws in workspaces"
          :key="ws.id"
          class="ws-editor__row"
          :class="{ 'ws-editor__row--active': wsStore.isActive(ws.id) }"
        >
          <div class="ws-editor__head">
            <input
              class="ws-editor__name"
              type="text"
              :value="nameDraft(ws)"
              :disabled="busyId === ws.id"
              placeholder="Workspace name"
              @input="nameDrafts[ws.id] = ($event.target as HTMLInputElement).value"
              @keydown.enter.prevent="rename(ws)"
              @blur="rename(ws)"
            />
            <span v-if="wsStore.isActive(ws.id)" class="ws-editor__badge ws-editor__badge--active">active</span>
            <span v-if="ws.dormant" class="ws-editor__badge ws-editor__badge--dormant">recovered</span>
            <div class="ws-editor__actions">
              <button
                v-if="!wsStore.isActive(ws.id)"
                type="button"
                class="btn-icon"
                style="font-size: 11px; padding: 3px 8px;"
                :disabled="busyId !== null"
                @click="activate(ws)"
              >{{ busyId === ws.id ? '…' : 'Activate' }}</button>
              <button
                type="button"
                class="btn-icon"
                style="font-size: 11px; padding: 3px 8px;"
                :title="ws.dormant ? 'Re-point this workspace to existing folders' : 'Add or change folders'"
                @click="emit('workspaces')"
              >{{ ws.dormant && ws.folders.length === 0 ? 'Re-point folders' : 'Edit folders' }}</button>
              <button
                type="button"
                class="btn-icon ws-editor__delete"
                style="font-size: 11px; padding: 3px 8px;"
                :disabled="wsStore.isActive(ws.id) || busyId !== null"
                :title="wsStore.isActive(ws.id) ? 'Switch to another workspace before deleting' : 'Delete this workspace'"
                @click="remove(ws)"
              >Delete</button>
            </div>
          </div>
          <div class="ws-editor__folders">
            <div
              v-if="ws.folders.length === 0"
              class="ws-editor__no-folders"
            >No folders. Re-point this workspace to existing folders to activate it.</div>
            <div
              v-for="path in ws.folders"
              :key="path"
              class="ws-editor__folder"
            >
              <span class="ws-editor__folder-path" :title="path">{{ path }}</span>
              <button
                type="button"
                class="btn-ghost ws-editor__folder-remove"
                :disabled="ws.folders.length <= 1 || busyId !== null"
                :title="ws.folders.length <= 1 ? 'A workspace needs at least one folder' : 'Remove folder'"
                @click="removeFolder(ws, path)"
              >&times;</button>
            </div>
          </div>
        </div>
      </div>

      <div style="display: flex; gap: 8px; align-items: center; margin-top: 10px;">
        <button
          type="button"
          class="btn-icon"
          style="font-size: 12px; padding: 4px 10px;"
          @click="emit('workspaces')"
        >New / switch workspace…</button>
      </div>
    </div>

    <div class="settings-section">
      <div style="margin-bottom: 8px; font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px;">
        Saved Workspace Groups
      </div>
      <div
        id="settings-workspace-groups"
        style="display: flex; flex-direction: column; gap: 8px; font-size: 12px; color: var(--text-secondary);"
      >
        <div
          v-if="workspaceGroups.length === 0"
          style="color: var(--text-muted); font-size: 11px"
        >Saved workspace groups will appear here after you switch boards.</div>
        <div
          v-for="group in workspaceGroups"
          :key="group.key"
          style="border: 1px solid var(--border); border-radius: 8px; padding: 8px; background: var(--bg-elevated); display: flex; flex-direction: column; gap: 8px;"
        >
          <div style="display: flex; align-items: center; justify-content: space-between; gap: 8px;">
            <div style="font-size: 12px; font-weight: 600;">
              {{ workspaceGroupLabel(group) }}
              <span
                v-if="isActiveGroup(group)"
                style="font-size: 10px; color: var(--text-muted); font-weight: 500; margin-left: 4px;"
              >Current</span>
            </div>
            <div style="display: flex; gap: 6px; align-items: center;">
              <button
                type="button"
                class="btn-icon"
                style="font-size: 11px; padding: 3px 8px;"
                :disabled="switchingKey !== null"
                @click="useGroup(group)"
              >{{ switchingKey === group.key ? 'Switching…' : 'Use' }}</button>
            </div>
          </div>
          <div style="display: flex; flex-direction: column; gap: 4px;">
            <div
              v-for="path in group.workspaces"
              :key="path"
              style="font-family: monospace; font-size: 11px; color: var(--text-muted); word-break: break-all;"
            >{{ path }}</div>
          </div>
          <!-- Per-group parallel overrides. 0/empty = use global default. -->
          <div class="group-limits">
            <label>
              <span>Max parallel</span>
              <input
                type="number"
                min="0"
                :value="group.max_parallel ?? ''"
                placeholder="default"
                :disabled="savingGroup === group.key"
                @change="saveGroupLimits(group, ($event.target as HTMLInputElement).valueAsNumber || null, group.max_test_parallel ?? null)"
              />
            </label>
            <label>
              <span>Max test parallel</span>
              <input
                type="number"
                min="0"
                :value="group.max_test_parallel ?? ''"
                placeholder="default"
                :disabled="savingGroup === group.key"
                @change="saveGroupLimits(group, group.max_parallel ?? null, ($event.target as HTMLInputElement).valueAsNumber || null)"
              />
            </label>
          </div>
        </div>
      </div>
      <div style="margin-top: 6px; font-size: 11px; color: var(--text-muted); line-height: 1.4;">
        Workspace groups are saved automatically when they become active, so you can switch back without rebuilding the same folder set.
      </div>
    </div>
  </div>
</template>

<style scoped>
.ws-editor {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.ws-editor__row {
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 8px;
  background: var(--bg-elevated);
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.ws-editor__row--active {
  border-color: var(--accent);
}
.ws-editor__head {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.ws-editor__name {
  flex: 1;
  min-width: 120px;
  font-size: 13px;
  font-weight: 600;
  padding: 4px 8px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-input);
  color: var(--text);
}
.ws-editor__badge {
  font-size: 9px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.4px;
  padding: 1px 5px;
  border-radius: 999px;
}
.ws-editor__badge--active {
  background: var(--accent);
  color: var(--accent-contrast, #fff);
}
.ws-editor__badge--dormant {
  background: #b8860b;
  color: #fff;
}
.ws-editor__actions {
  display: flex;
  gap: 6px;
  margin-left: auto;
}
.ws-editor__delete:not(:disabled) {
  color: #c44;
}
.ws-editor__folders {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.ws-editor__no-folders {
  font-size: 11px;
  color: #b8860b;
}
.ws-editor__folder {
  display: flex;
  align-items: center;
  gap: 6px;
}
.ws-editor__folder-path {
  font-family: monospace;
  font-size: 11px;
  color: var(--text-muted);
  word-break: break-all;
  flex: 1;
}
.ws-editor__folder-remove {
  flex-shrink: 0;
  font-size: 13px;
  line-height: 1;
  padding: 0 6px;
}
.group-limits {
  display: flex;
  gap: 12px;
  border-top: 1px dashed var(--border);
  padding-top: 6px;
  margin-top: 2px;
}
.group-limits label {
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-size: 11px;
  color: var(--text-muted);
}
.group-limits input {
  width: 80px;
  padding: 3px 6px;
  font-size: 12px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: var(--bg-input);
  color: var(--text);
}
</style>
