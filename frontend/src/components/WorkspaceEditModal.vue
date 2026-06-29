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
import { useFolderBrowser, type BrowseEntry } from '../composables/useFolderBrowser';
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

const {
  browsePath, pathInput, browseEntries, browseLoading, browseError, filter, showHidden,
  browse, navigateUp, navigateInto, goToPath, onPathKeydown, shortenPath, breadcrumbSegments,
} = useFolderBrowser();

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
  if (!w || busy.value || wsStore.isActive(w.id)) return;
  const ok = await dialog.confirm({
    title: 'Delete workspace',
    message: `Delete "${workspaceLabel(w.name, w.folders)}"? Folders on disk are not touched, but the workspace will no longer be reachable until recreated.`,
    confirmLabel: 'Delete',
    danger: true,
  });
  if (!ok) return;
  busy.value = true;
  try {
    await wsStore.remove(w.id);
    toast.push('Deleted workspace', { kind: 'success' });
    close();
  } catch (e) {
    setStatus('Error: ' + (e instanceof Error ? e.message : String(e)));
    busy.value = false;
  }
}

function filteredEntries() {
  let entries = browseEntries.value;
  if (!showHidden.value) entries = entries.filter(e => !e.name.startsWith('.'));
  const f = filter.value.trim().toLowerCase();
  if (f) entries = entries.filter(e => e.name.toLowerCase().includes(f));
  const added = (e: BrowseEntry) => ws.value?.folders.includes(e.path) ?? false;
  return [...entries].sort((a, b) => Number(added(a)) - Number(added(b)));
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
      class="modal-card ws-picker ws-edit"
      role="dialog"
      aria-modal="true"
      aria-label="Workspace settings"
    >
      <div class="ws-picker__header">
        <div style="flex: 1; min-width: 0">
          <h3 class="ws-picker__title">Workspace settings</h3>
          <p class="ws-picker__subtitle">{{ workspaceLabel(ws.name, ws.folders) }}</p>
        </div>
        <span class="ws-picker__apply-status">{{ status }}</span>
        <button type="button" class="btn-ghost ws-picker__close" @click="close">&times;</button>
      </div>

      <div class="ws-edit__body">
        <!-- Name: a single labeled field (no duplicated label/input pair). -->
        <div class="ws-edit__field">
          <label class="ws-edit__label" for="ws-edit-name">Name</label>
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

        <!-- Parallel caps: empty input clears (global default), a number sets. -->
        <div class="ws-edit__field">
          <span class="ws-edit__label">Parallel limits</span>
          <div class="ws-edit__caps">
            <label>
              <span>Max parallel</span>
              <input
                type="number"
                min="0"
                :value="ws.max_parallel ?? ''"
                placeholder="default"
                :disabled="busy"
                @change="saveCap('max_parallel', $event)"
              />
            </label>
            <label>
              <span>Max test parallel</span>
              <input
                type="number"
                min="0"
                :value="ws.max_test_parallel ?? ''"
                placeholder="default"
                :disabled="busy"
                @change="saveCap('max_test_parallel', $event)"
              />
            </label>
          </div>
        </div>

        <!-- Folders: list with remove + a reveal-on-demand browser to add more. -->
        <div class="ws-edit__field">
          <div class="ws-edit__folders-head">
            <span class="ws-edit__label">Folders</span>
            <button
              type="button"
              class="btn-ghost ws-picker__add-folder-btn"
              @click="toggleBrowser"
            >{{ showBrowser ? 'Done adding' : '+ Add folder' }}</button>
          </div>
          <div class="ws-edit__folder-list">
            <div
              v-for="path in ws.folders"
              :key="path"
              class="ws-selected-item"
            >
              <span class="ws-selected-item__path" :title="path">{{ shortenPath(path) }}</span>
              <button
                type="button"
                class="btn-ghost ws-selected-item__remove"
                :disabled="ws.folders.length <= 1 || busy"
                :title="ws.folders.length <= 1 ? 'A workspace needs at least one folder' : 'Remove folder'"
                @click="removeFolder(path)"
              >&times;</button>
            </div>
            <div
              v-if="ws.folders.length === 0"
              style="font-size: 11px; color: var(--text-muted); padding: 4px 2px"
            >No folders. Add one below.</div>
          </div>

          <!-- Reveal-on-demand folder browser (shared with the picker). Capped
               height so it never grows the modal past a normal centered card. -->
          <div v-if="showBrowser" class="ws-edit__browser">
            <div class="ws-picker__path-row">
              <input
                v-model="pathInput"
                class="field ws-picker__path-input"
                type="text"
                placeholder="/absolute/path"
                autocomplete="off"
                @keydown="onPathKeydown"
              />
              <button type="button" class="btn-icon ws-picker__go-btn" @click="goToPath">Go</button>
            </div>
            <div class="ws-picker__breadcrumb">
              <template v-for="(seg, i) in breadcrumbSegments()" :key="seg.path">
                <span v-if="seg.label === '/'" style="color: var(--text-muted)">/</span>
                <template v-else>
                  <span v-if="i > 1" style="color: var(--text-muted)">/</span>
                  <button
                    type="button"
                    :style="{
                      border: 'none', background: 'none',
                      color: i === breadcrumbSegments().length - 1 ? 'var(--text)' : 'var(--accent)',
                      cursor: 'pointer', fontSize: '12px', padding: 0,
                      fontWeight: i === breadcrumbSegments().length - 1 ? 600 : 400,
                    }"
                    @click="browse(seg.path)"
                  >{{ seg.label }}</button>
                </template>
              </template>
            </div>
            <div class="ws-picker__browser-toolbar">
              <label class="ws-picker__toggle">
                <input v-model="showHidden" type="checkbox" />
                Show hidden
              </label>
              <button
                type="button"
                class="btn-ghost ws-picker__add-folder-btn"
                :disabled="ws.folders.includes(browsePath) || busy"
                @click="addFolder(browsePath)"
              >+ Add current folder</button>
            </div>
            <div class="ws-picker__status">
              <span v-if="browseLoading">Loading...</span>
              <span v-else-if="browseError" style="color: #c44">{{ browseError }}</span>
            </div>
            <div class="ws-picker__list ws-edit__list">
              <div class="ws-picker__filter-wrap">
                <input
                  v-model="filter"
                  class="field ws-picker__filter"
                  type="search"
                  placeholder="Filter..."
                  autocomplete="off"
                />
              </div>
              <button
                v-if="browsePath !== '/'"
                type="button"
                class="ws-entry--parent"
                @click="navigateUp"
              ><span>..</span></button>
              <div
                v-for="entry in filteredEntries()"
                :key="entry.path"
                class="ws-entry"
              >
                <button
                  type="button"
                  class="ws-entry__name"
                  :title="entry.path"
                  @click="navigateInto(entry)"
                >
                  <span style="overflow: hidden; text-overflow: ellipsis">{{ entry.name }}</span>
                  <span v-if="entry.is_git_repo" class="ws-entry__badge">git</span>
                </button>
                <button
                  v-if="!ws.folders.includes(entry.path)"
                  type="button"
                  class="btn-ghost ws-entry__add"
                  :disabled="busy"
                  @click="addFolder(entry.path)"
                >+ Add</button>
                <span v-else class="ws-entry__added">added</span>
              </div>
              <div
                v-if="!browseLoading && filteredEntries().length === 0 && browsePath !== '/'"
                style="padding: 8px; font-size: 11px; color: var(--text-muted)"
              >{{ filter.trim() ? 'No matches.' : 'Empty.' }}</div>
            </div>
          </div>
        </div>

        <!-- Delete: the server 409s on the active workspace; disable + explain. -->
        <div class="ws-edit__danger">
          <button
            type="button"
            class="btn ws-edit__delete"
            :disabled="wsStore.isActive(ws.id) || busy"
            :title="wsStore.isActive(ws.id) ? 'Switch to another workspace before deleting' : 'Delete this workspace'"
            @click="remove"
          >Delete workspace</button>
          <span v-if="wsStore.isActive(ws.id)" class="ws-edit__danger-note">
            Active workspace — switch away to delete.
          </span>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* A normal centered modal, not the picker's 1000px two-column grid. Override
   .ws-picker's width/padding (scoped wins over the global single-class rule),
   keeping its button/entry polish. */
.ws-edit {
  max-width: 560px;
  padding: 0;
}
.ws-edit__body {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 16px;
  max-height: 70vh;
  overflow-y: auto;
}
.ws-edit__field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.ws-edit__label {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.ws-edit__caps {
  display: flex;
  gap: 12px;
}
.ws-edit__caps label {
  display: flex;
  flex-direction: column;
  gap: 3px;
  font-size: 11px;
  color: var(--text-muted);
}
.ws-edit__caps input {
  width: 110px;
  padding: 5px 8px;
  font-size: 12px;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: var(--bg-input);
  color: var(--text);
}
.ws-edit__folders-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.ws-edit__folder-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.ws-edit__browser {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 4px;
  padding: 10px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-elevated);
}
/* Cap the browse list so the reveal never blows out the modal height. */
.ws-edit__list {
  max-height: 220px;
}
.ws-picker__close {
  font-size: 18px;
  line-height: 1;
  padding: 4px 9px;
  flex-shrink: 0;
  border-radius: 8px;
}
.ws-edit__danger {
  display: flex;
  align-items: center;
  gap: 10px;
  border-top: 1px solid var(--border);
  padding-top: 14px;
}
.ws-edit__delete {
  font-size: 12px;
  padding: 6px 12px;
  border: 1px solid color-mix(in oklab, #c44 50%, var(--border));
  color: #c44;
  background: transparent;
  border-radius: 8px;
}
.ws-edit__delete:disabled {
  opacity: 0.45;
  cursor: default;
}
.ws-edit__delete:not(:disabled):hover {
  background: color-mix(in oklab, #c44 12%, transparent);
}
.ws-edit__danger-note {
  font-size: 11px;
  color: var(--text-muted);
}
</style>
