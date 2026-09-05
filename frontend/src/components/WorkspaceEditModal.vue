<script setup lang="ts">
// Per-workspace settings popup. Edits one workspace's name, folder set, and
// parallel caps, and offers deletion — the single place workspace settings are
// managed now that the Settings → Workspace tab is gone. Opened from the sidebar
// switcher and the picker's per-row Edit via ui.openWorkspaceEdit(id).
//
// The target workspace is derived live from the registry by id (never snapshot)
// so the inputs stay in sync after wsStore.update swaps the DTO, and the modal
// tears itself down cleanly when the workspace is deleted out from under it.
import { computed, onBeforeUnmount, ref, watch } from 'vue';

import { useWorkspacesStore } from '../stores/workspaces';
import { useUiStore } from '../stores/ui';
import { useDialogStore } from '../stores/dialog';
import { useToastStore } from '../stores/toast';
import { useFocusTrap } from '../composables/useFocusTrap';
import { useFolderBrowser } from '../composables/useFolderBrowser';
import FolderBrowser from './FolderBrowser.vue';
import { workspaceLabel } from '../lib/workspaceLabel';

const wsStore = useWorkspacesStore();
const ui = useUiStore();
const dialog = useDialogStore();
const toast = useToastStore();

// Live view of the workspace being edited. Deriving (rather than snapshotting)
// keeps folders/caps fresh after each update and lets the v-if guard close the
// modal when a delete removes the row.
const ws = computed(() => wsStore.workspaces.find(w => w.id === ui.editWorkspaceId) ?? null);

const cardRef = ref<HTMLElement | null>(null);
useFocusTrap(cardRef, computed(() => ws.value !== null));

const busy = ref(false);
const status = ref('');

// Name is a local draft so a half-typed rename isn't clobbered by a DTO refresh;
// it's persisted on blur/Enter. Caps and folders persist immediately on change.
const nameDraft = ref(ws.value?.name ?? '');
watch(() => ui.editWorkspaceId, () => { nameDraft.value = ws.value?.name ?? ''; showBrowser.value = false; });
// If the workspace vanishes (deleted elsewhere) while open, close cleanly.
watch(ws, (w) => { if (!w && ui.editWorkspaceId) close(); });

const browser = useFolderBrowser();
const { browseEntries, browseError, browse, shortenPath } = browser;

const showBrowser = ref(false);

function setStatus(msg: string) {
  status.value = msg;
  if (msg === 'Saved.') setTimeout(() => { if (status.value === 'Saved.') status.value = ''; }, 1500);
}

function close() {
  ui.closeWorkspaceEdit();
}

async function saveName() {
  const w = ws.value;
  if (!w || busy.value) return;
  const next = nameDraft.value.trim();
  if (next === (w.name ?? '')) return;
  busy.value = true;
  status.value = '';
  try {
    await wsStore.update(w.id, { name: next });
    setStatus('Saved.');
  } catch (e) {
    setStatus('Error: ' + (e instanceof Error ? e.message : String(e)));
  } finally {
    busy.value = false;
  }
}

// Parallel caps: a number sets the cap, an empty input clears it (null) so the
// global default applies again. Both write through PUT /api/workspaces/{id}.
async function saveCap(field: 'max_parallel' | 'max_test_parallel', e: Event) {
  const w = ws.value;
  if (!w || busy.value) return;
  const raw = (e.target as HTMLInputElement).value.trim();
  const value = raw === '' ? null : Number(raw);
  if (value !== null && (!Number.isFinite(value) || value < 0)) return;
  busy.value = true;
  status.value = '';
  try {
    await wsStore.update(w.id, { [field]: value });
    setStatus('Saved.');
  } catch (err) {
    setStatus('Error: ' + (err instanceof Error ? err.message : String(err)));
  } finally {
    busy.value = false;
  }
}

function toggleBrowser() {
  showBrowser.value = !showBrowser.value;
  // Lazy first listing: only hit the backend once the browser is revealed.
  if (showBrowser.value && browseEntries.value.length === 0 && !browseError.value) {
    void browse('');
  }
}

async function addFolder(path: string) {
  const w = ws.value;
  if (!w || busy.value || w.folders.includes(path)) return;
  busy.value = true;
  status.value = '';
  try {
    await wsStore.update(w.id, { folders: [...w.folders, path] });
    setStatus('Saved.');
  } catch (e) {
    setStatus('Error: ' + (e instanceof Error ? e.message : String(e)));
  } finally {
    busy.value = false;
  }
}

// The server rejects an empty folder set (400), so the last folder cannot be
// removed here; the button is disabled when only one remains.
async function removeFolder(path: string) {
  const w = ws.value;
  if (!w || busy.value || w.folders.length <= 1) return;
  busy.value = true;
  status.value = '';
  try {
    await wsStore.update(w.id, { folders: w.folders.filter(f => f !== path) });
    setStatus('Saved.');
  } catch (e) {
    setStatus('Error: ' + (e instanceof Error ? e.message : String(e)));
  } finally {
    busy.value = false;
  }
}

async function remove() {
  const w = ws.value;
  if (!w || busy.value) return;
  const active = wsStore.isActive(w.id);
  const ok = await dialog.confirm({
    title: 'Delete workspace',
    message:
      `Permanently delete "${workspaceLabel(w.name, w.folders)}" and wipe all its task history, ` +
      `transcripts, and session data? Your folders on disk are not touched, but this cannot be undone.` +
      (active ? ' The board will switch to another workspace.' : ''),
    confirmLabel: 'Delete',
    danger: true,
  });
  if (!ok) return;
  busy.value = true;
  try {
    await wsStore.remove(w.id);
    toast.push('Deleted workspace', { kind: 'success' });
    close();
    // If that was the last workspace, prompt the user to create one.
    if (wsStore.workspaces.length === 0) ui.openWorkspaces();
  } catch (e) {
    setStatus('Error: ' + (e instanceof Error ? e.message : String(e)));
    busy.value = false;
  }
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') close();
}
document.addEventListener('keydown', onKey);
onBeforeUnmount(() => document.removeEventListener('keydown', onKey));

function onBackdrop(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('modal-overlay')) close();
}
</script>

<template>
  <div
    v-if="ws"
    class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4"
    @click="onBackdrop"
  >
    <div
      ref="cardRef"
      class="dialog dialog--wide ws-picker ws-edit"
      role="dialog"
      aria-modal="true"
      aria-label="Workspace settings"
    >
      <div class="dialog-head ws-picker__header">
        <div class="dialog-head__main">
          <h3 class="dialog-title ws-picker__title">Workspace settings</h3>
          <p class="dialog-sub">{{ workspaceLabel(ws.name, ws.folders) }}</p>
        </div>
        <span class="ws-edit__status ws-picker__apply-status">{{ status }}</span>
        <button type="button" class="icon-btn ws-picker__close" aria-label="Close" @click="close">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><line x1="6" y1="6" x2="18" y2="18"></line><line x1="18" y1="6" x2="6" y2="18"></line></svg>
        </button>
      </div>

      <div class="dialog-body ws-edit__body">
        <!-- Name and parallel caps: one card of rows, each with its field at
             the end. An empty cap clears it so the global default applies. -->
        <div class="card compact">
          <div class="rows">
            <div class="row">
              <label class="row-main" for="ws-edit-name">
                <span class="row-title">Name</span>
                <span class="row-help">Shown in the rail and the picker.</span>
              </label>
              <div class="row-end ws-edit__end">
                <input
                  id="ws-edit-name"
                  v-model="nameDraft"
                  class="field"
                  type="text"
                  :placeholder="workspaceLabel('', ws.folders)"
                  autocomplete="off"
                  @keydown.enter.prevent="saveName"
                  @blur="saveName"
                />
              </div>
            </div>
            <div class="row ws-edit__caps">
              <div class="row-main">
                <span class="row-title">Max parallel</span>
                <span class="row-help">Tasks running at once. Empty uses the global limit.</span>
              </div>
              <div class="row-end ws-edit__end ws-edit__end--num">
                <input
                  class="field"
                  type="number"
                  min="0"
                  :value="ws.max_parallel ?? ''"
                  placeholder="default"
                  :disabled="busy"
                  aria-label="Max parallel"
                  @change="saveCap('max_parallel', $event)"
                />
              </div>
            </div>
            <div class="row ws-edit__caps">
              <div class="row-main">
                <span class="row-title">Max test parallel</span>
                <span class="row-help">Test runs at once. Empty uses the global limit.</span>
              </div>
              <div class="row-end ws-edit__end ws-edit__end--num">
                <input
                  class="field"
                  type="number"
                  min="0"
                  :value="ws.max_test_parallel ?? ''"
                  placeholder="default"
                  :disabled="busy"
                  aria-label="Max test parallel"
                  @change="saveCap('max_test_parallel', $event)"
                />
              </div>
            </div>
          </div>
        </div>

        <!-- Folders: the list with remove, and a reveal-on-demand browser. -->
        <div class="card compact ws-edit__folders">
          <div class="card-head">
            <span class="eyebrow">Folders</span>
            <button
              type="button"
              class="btn sm ghost ws-picker__add-folder-btn"
              @click="toggleBrowser"
            >{{ showBrowser ? 'Done adding' : '+ Add folder' }}</button>
          </div>
          <div class="card-pad ws-edit__folder-list">
            <div
              v-for="path in ws.folders"
              :key="path"
              class="ws-selected-item"
            >
              <span class="ws-selected-item__path" :title="path">{{ shortenPath(path) }}</span>
              <button
                type="button"
                class="icon-btn sm ws-selected-item__remove"
                :disabled="ws.folders.length <= 1 || busy"
                :title="ws.folders.length <= 1 ? 'A workspace needs at least one folder' : 'Remove folder'"
                aria-label="Remove folder"
                @click="removeFolder(path)"
              >
                <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><line x1="6" y1="6" x2="18" y2="18"></line><line x1="18" y1="6" x2="6" y2="18"></line></svg>
              </button>
            </div>
            <div v-if="ws.folders.length === 0" class="ws-picker__empty">No folders. Add one below.</div>
            <FolderBrowser
              v-if="showBrowser"
              class="ws-edit__browser"
              :browser="browser"
              :added="ws.folders"
              :busy="busy"
              @add="addFolder"
            />
          </div>
        </div>

        <!-- Delete: the server 409s on the active workspace; explain the switch. -->
        <div class="ws-edit__danger">
          <button
            type="button"
            class="btn ghost danger ws-edit__delete"
            :disabled="busy"
            title="Permanently delete this workspace and wipe its data"
            @click="remove"
          >Delete workspace</button>
          <span class="dialog-note ws-edit__danger-note">
            Wipes all task history and session data.{{ ws && wsStore.isActive(ws.id) ? ' Switches the board to another workspace.' : '' }}
          </span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.ws-edit__status {
  align-self: center;
  font-size: var(--fs-10);
  color: var(--ink-3);
}
.ws-edit__end {
  width: 240px;
}
.ws-edit__end--num {
  width: 120px;
}
.ws-edit__folders {
  margin-top: 12px;
}
.ws-edit__folder-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.ws-edit__browser {
  margin-top: 6px;
}
/* Cap the browse list so the reveal never grows the dialog past a normal card. */
.ws-edit__browser :deep(.fb__list) {
  max-height: 240px;
}
.ws-edit__danger {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 16px;
  padding-top: 14px;
  border-top: 1px solid var(--rule);
}
</style>
