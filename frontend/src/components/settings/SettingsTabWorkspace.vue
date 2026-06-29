<script setup lang="ts">
import { ref, computed, onMounted, reactive } from 'vue';
import { useWorkspacesStore } from '../../stores/workspaces';
import { useDialogStore } from '../../stores/dialog';
import { workspaceLabel } from '../../lib/workspaceLabel';
import type { Workspace } from '../../api/types';

const emit = defineEmits<{ workspaces: [] }>();
const wsStore = useWorkspacesStore();
const dialog = useDialogStore();
const status = ref('');

// busyId tracks the workspace currently mid-mutation so its row controls
// disable without freezing the whole list.
const busyId = ref<string | null>(null);
// nameDrafts holds the in-progress rename text per workspace id, seeded lazily
// from the registry so an unsaved edit is not clobbered by a refresh.
const nameDrafts = reactive<Record<string, string>>({});

const workspaces = computed<Workspace[]>(() => wsStore.workspaces);

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

// Per-workspace parallel overrides write through PUT /api/workspaces/{id}. A
// number sets the cap; null (empty input) clears it so the global default takes
// over again. The store replaces the local DTO from the response, so the inputs
// reflect the saved value.
async function saveLimits(ws: Workspace, maxParallel: number | null, maxTestParallel: number | null) {
  if (busyId.value) return;
  busyId.value = ws.id;
  status.value = '';
  try {
    await wsStore.update(ws.id, { max_parallel: maxParallel, max_test_parallel: maxTestParallel });
    setStatus('Saved.');
  } catch (e) {
    setStatus('Error: ' + (e instanceof Error ? e.message : String(e)));
  } finally {
    busyId.value = null;
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
            <span class="ws-editor__label">{{ workspaceLabel(ws.name, ws.folders) }}</span>
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
          <input
            class="ws-editor__name"
            type="text"
            :value="nameDraft(ws)"
            :disabled="busyId === ws.id"
            :placeholder="workspaceLabel(ws.name, ws.folders)"
            aria-label="Rename workspace"
            @input="nameDrafts[ws.id] = ($event.target as HTMLInputElement).value"
            @keydown.enter.prevent="rename(ws)"
            @blur="rename(ws)"
          />
          <!-- Per-workspace parallel overrides. Empty input = global default. -->
          <div class="ws-editor__limits">
            <label>
              <span>Max parallel</span>
              <input
                type="number"
                min="0"
                :value="ws.max_parallel ?? ''"
                placeholder="default"
                :disabled="busyId === ws.id"
                @change="saveLimits(ws, ($event.target as HTMLInputElement).valueAsNumber || null, ws.max_test_parallel ?? null)"
              />
            </label>
            <label>
              <span>Max test parallel</span>
              <input
                type="number"
                min="0"
                :value="ws.max_test_parallel ?? ''"
                placeholder="default"
                :disabled="busyId === ws.id"
                @change="saveLimits(ws, ws.max_parallel ?? null, ($event.target as HTMLInputElement).valueAsNumber || null)"
              />
            </label>
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
.ws-editor__label {
  font-size: 13px;
  font-weight: 600;
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}
.ws-editor__name {
  width: 100%;
  font-size: 12px;
  font-weight: 500;
  padding: 4px 8px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-input);
  color: var(--text);
}
.ws-editor__name::placeholder {
  color: var(--text-muted);
  font-weight: 400;
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
.ws-editor__limits {
  display: flex;
  gap: 12px;
  border-top: 1px dashed var(--border);
  padding-top: 6px;
  margin-top: 2px;
}
.ws-editor__limits label {
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-size: 11px;
  color: var(--text-muted);
}
.ws-editor__limits input {
  width: 80px;
  padding: 3px 6px;
  font-size: 12px;
  border: 1px solid var(--border);
  border-radius: 4px;
  background: var(--bg-input);
  color: var(--text);
}
</style>
