<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import { api } from '../api/client';
import { useTaskStore } from '../stores/tasks';
import { useWorkspacesStore } from '../stores/workspaces';
import { useUiStore } from '../stores/ui';
import { useDialogStore } from '../stores/dialog';
import { useToastStore } from '../stores/toast';
import { useFocusTrap } from '../composables/useFocusTrap';
import { useFolderBrowser, type BrowseEntry } from '../composables/useFolderBrowser';
import { workspaceLabel } from '../lib/workspaceLabel';
import type { Workspace } from '../api/types';

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();

const store = useTaskStore();
const wsStore = useWorkspacesStore();
const ui = useUiStore();
const dialog = useDialogStore();
const toast = useToastStore();

// Create a new folder under the current browse path, then refresh the listing.
async function createFolder() {
  const name = await dialog.prompt({ title: 'New folder', message: `Create a folder inside ${browsePath.value}:`, initial: '' });
  if (!name) return;
  try {
    await api('POST', '/api/workspaces/mkdir', { path: browsePath.value, name: name.trim() });
    await browse(browsePath.value);
    toast.push(`Created ${name.trim()}`, { kind: 'success' });
  } catch (e) {
    toast.push(`Create failed: ${e instanceof Error ? e.message : String(e)}`, { kind: 'error' });
  }
}

// Rename a browsed directory entry, then refresh the listing.
async function renameEntry(entry: { path: string; name: string }) {
  const name = await dialog.prompt({ title: 'Rename folder', message: `Rename "${entry.name}" to:`, initial: entry.name });
  if (!name || name.trim() === entry.name) return;
  try {
    await api('POST', '/api/workspaces/rename', { path: entry.path, name: name.trim() });
    await browse(browsePath.value);
    toast.push(`Renamed to ${name.trim()}`, { kind: 'success' });
  } catch (e) {
    toast.push(`Rename failed: ${e instanceof Error ? e.message : String(e)}`, { kind: 'error' });
  }
}

const cardRef = ref<HTMLElement | null>(null);
useFocusTrap(cardRef, computed(() => props.modelValue));

// view splits the modal into two surfaces: 'list' picks an existing workspace
// to activate; 'wizard' is the create/edit folder-browse flow. The default is
// 'wizard' so a fresh mount (no registry loaded) lands on the folder browser —
// the first-run experience and the shape the wizard tests assert against.
const view = ref<'list' | 'wizard'>('wizard');
// editingId is the workspace being re-pointed; null means the wizard creates a
// brand new workspace on confirm.
const editingId = ref<string | null>(null);
const wsName = ref('');

const folders = ref<string[]>([]);
const {
  browsePath, pathInput, browseEntries, browseLoading, browseError, filter, showHidden,
  browse, navigateUp, navigateInto, goToPath, onPathKeydown, shortenPath, breadcrumbSegments,
} = useFolderBrowser();
const saving = ref(false);
const applyStatus = ref('');
const activatingId = ref<string | null>(null);

// Two step wizard: 1 = choose folders, 2 = name, review and activate.
const step = ref(1);
const canProceed = computed(() => folders.value.length > 0);

function goNext() {
  if (!canProceed.value) return;
  step.value = 2;
}

function goBack() {
  step.value = 1;
}

const existing = computed<Workspace[]>(() => wsStore.workspaces);

// Folder basenames make a readable fallback label for unnamed workspaces.
function basenames(paths: string[]): string {
  return paths
    .map(p => {
      const clean = String(p || '').replace(/[\\/]+$/, '');
      const parts = clean.split(/[\\/]/);
      return parts[parts.length - 1] || clean;
    })
    .join(', ');
}

// Enter the wizard to create a new workspace from scratch. Returns the browse
// promise so callers (the open watcher) can await the first listing.
function enterNewWorkspace(): Promise<void> {
  editingId.value = null;
  wsName.value = '';
  folders.value = [];
  step.value = 1;
  view.value = 'wizard';
  return browse('');
}

// Enter the wizard to re-point an existing workspace's folders (used for
// dormant workspaces recovered without folders, and the generic edit path).
function enterEdit(ws: Workspace) {
  editingId.value = ws.id;
  wsName.value = ws.name ?? '';
  folders.value = [...ws.folders];
  step.value = 1;
  view.value = 'wizard';
  browse('');
}

// Activate an existing workspace and close. Dormant workspaces missing folders
// cannot run a board, so route the user into editing folders first.
async function activateExisting(ws: Workspace) {
  if (activatingId.value) return;
  if (ws.dormant && ws.folders.length === 0) {
    enterEdit(ws);
    return;
  }
  activatingId.value = ws.id;
  applyStatus.value = '';
  try {
    await wsStore.activate(ws.id);
    close();
  } catch (e) {
    applyStatus.value = e instanceof Error ? e.message : 'Failed to activate';
  } finally {
    activatingId.value = null;
  }
}

watch(
  () => props.modelValue,
  async (open) => {
    if (!open) return;
    browsePath.value = '';
    pathInput.value = '';
    browseError.value = '';
    filter.value = '';
    applyStatus.value = '';
    editingId.value = null;
    wsName.value = '';
    folders.value = [];
    step.value = 1;
    // Default to the create wizard and kick off the first directory listing
    // immediately; the registry load runs concurrently and promotes the modal
    // to the list view when workspaces already exist (no folder-browse flash on
    // first run, which is the common path when there are none).
    view.value = 'wizard';
    document.addEventListener('keydown', onKey);
    const browseP = browse('');
    await wsStore.list();
    if (existing.value.length > 0) {
      view.value = 'list';
    }
    await browseP;
  },
);

watch(
  () => props.modelValue,
  (open) => {
    if (!open) document.removeEventListener('keydown', onKey);
  },
);

onBeforeUnmount(() => {
  document.removeEventListener('keydown', onKey);
});

function addFolder(path: string) {
  if (!folders.value.includes(path)) {
    folders.value.push(path);
  }
}

function removeFolder(index: number) {
  folders.value.splice(index, 1);
}

function clearSelection() {
  folders.value = [];
}

// Effective workspace name: the typed name, or a basename fallback so the
// created workspace is never blank.
function effectiveName(): string {
  return wsName.value.trim() || basenames(folders.value);
}

// Confirm the wizard: create a new workspace (or re-point the one being
// edited), then activate it so the board switches immediately. Replaces the
// legacy path-based PUT /api/workspaces switch.
async function confirm() {
  if (!folders.value.length) return;
  saving.value = true;
  applyStatus.value = 'Applying...';
  try {
    let id = editingId.value;
    if (id) {
      await wsStore.update(id, { name: effectiveName(), folders: [...folders.value] });
    } else {
      const created = await wsStore.create(effectiveName(), [...folders.value]);
      id = created.id;
    }
    await wsStore.activate(id);
    applyStatus.value = '';
    close();
  } catch (e) {
    console.error('save workspace:', e);
    applyStatus.value = e instanceof Error ? e.message : 'Failed to apply';
  } finally {
    saving.value = false;
  }
}

function backToList() {
  if (existing.value.length === 0) return;
  view.value = 'list';
}

function close() {
  emit('update:modelValue', false);
}

// First run (no persisted workspaces) forces a selection: the close control,
// backdrop click, and Escape are all suppressed until a workspace exists.
const dismissable = computed(() => (store.config?.workspaces?.length ?? 0) > 0);

function onBackdrop(e: MouseEvent) {
  if (!dismissable.value) return;
  if ((e.target as HTMLElement).classList.contains('modal-overlay')) close();
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape' && dismissable.value) close();
}

function filteredEntries() {
  let entries = browseEntries.value;
  if (!showHidden.value) {
    entries = entries.filter((e) => !e.name.startsWith('.'));
  }
  const f = filter.value.trim().toLowerCase();
  if (f) {
    entries = entries.filter((e) => e.name.toLowerCase().includes(f));
  }
  // Already-added folders sink to the bottom so the list stays a queue of
  // things still left to add; relative order within each group is preserved.
  const added = (e: BrowseEntry) => folders.value.includes(e.path);
  return [...entries].sort((a, b) => Number(added(a)) - Number(added(b)));
}
</script>

<template>
  <div
    v-if="modelValue"
    class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4"
    @click="onBackdrop"
  >
    <div ref="cardRef" class="modal-card ws-picker" role="dialog" aria-modal="true" aria-label="Workspace Picker">
      <div class="ws-picker__header">
        <div style="flex: 1; min-width: 0">
          <h3 class="ws-picker__title">
            {{ view === 'list' ? 'Select Workspace' : editingId ? 'Edit Workspace' : 'New Workspace' }}
          </h3>
        </div>
        <button
          v-if="dismissable"
          type="button"
          class="btn-ghost ws-picker__close"
          @click="close"
        >
          &times;
        </button>
      </div>

      <!-- List view: pick an existing workspace to activate. -->
      <div v-if="view === 'list'" class="ws-picker__body ws-picker__list-view">
        <p class="ws-step__instruction">
          Click a workspace to switch the board to it. Editing folders never loses history.
        </p>
        <div class="ws-list">
          <div
            v-for="ws in existing"
            :key="ws.id"
            class="ws-list__item"
            :class="{ 'ws-list__item--active': wsStore.isActive(ws.id) }"
          >
            <button
              type="button"
              class="ws-list__main"
              :disabled="activatingId !== null"
              @click="activateExisting(ws)"
            >
              <span class="ws-list__name">
                {{ workspaceLabel(ws.name, ws.folders) }}
                <span v-if="wsStore.isActive(ws.id)" class="ws-list__badge ws-list__badge--active">active</span>
                <span v-if="ws.dormant" class="ws-list__badge ws-list__badge--dormant">recovered</span>
              </span>
              <span class="ws-list__paths" :title="ws.folders.join('\n')">
                {{ ws.folders.length ? ws.folders.map(shortenPath).join('  ·  ') : 'No folders — re-point to use' }}
              </span>
            </button>
            <button
              type="button"
              class="btn-ghost ws-list__edit"
              title="Edit name, folders, and limits"
              @click="close(); ui.openWorkspaceEdit(ws.id)"
            >Edit</button>
          </div>
        </div>
        <div class="ws-step__footer">
          <span class="ws-picker__apply-status">{{ applyStatus }}</span>
          <button type="button" class="btn btn-accent" @click="enterNewWorkspace">
            + New workspace
          </button>
        </div>
      </div>

      <template v-else>
        <div class="ws-stepper" role="tablist" aria-label="Workspace setup steps">
          <button
            type="button"
            class="ws-step"
            :class="{ 'ws-step--active': step === 1, 'ws-step--done': step > 1 }"
            @click="goBack"
          >
            <span class="ws-step__circle">1</span>
            <span class="ws-step__label">Choose folders</span>
          </button>
          <span class="ws-step__connector" :class="{ 'ws-step__connector--done': step > 1 }"></span>
          <button
            type="button"
            class="ws-step"
            :class="{ 'ws-step--active': step === 2, 'ws-step--upcoming': step < 2 }"
            :disabled="!canProceed"
            @click="goNext"
          >
            <span class="ws-step__circle">2</span>
            <span class="ws-step__label">Name &amp; activate</span>
          </button>
        </div>

        <div v-show="step === 1" class="ws-picker__body ws-picker__body--step">
        <p class="ws-step__instruction">
          Browse to your project directories and click + Add. Git repos are marked.
          Add as many as you want.
        </p>
        <div class="ws-picker__browser">
          <div class="ws-picker__path-row">
            <input
              v-model="pathInput"
              class="field ws-picker__path-input"
              type="text"
              placeholder="/absolute/path"
              autocomplete="off"
              @keydown="onPathKeydown"
            />
            <button type="button" class="btn-icon ws-picker__go-btn" @click="goToPath">
              Go
            </button>
          </div>

          <div class="ws-picker__breadcrumb">
            <template v-for="(seg, i) in breadcrumbSegments()" :key="seg.path">
              <span v-if="seg.label === '/'" style="color: var(--text-muted)">/</span>
              <template v-else>
                <span v-if="i > 1" style="color: var(--text-muted)">/</span>
                <button
                  type="button"
                  :style="{
                    border: 'none',
                    background: 'none',
                    color: i === breadcrumbSegments().length - 1 ? 'var(--text)' : 'var(--accent)',
                    cursor: 'pointer',
                    fontSize: '12px',
                    padding: 0,
                    fontWeight: i === breadcrumbSegments().length - 1 ? 600 : 400,
                  }"
                  @click="browse(seg.path)"
                >
                  {{ seg.label }}
                </button>
              </template>
            </template>
          </div>

          <div class="ws-picker__browser-toolbar">
            <label class="ws-picker__toggle">
              <input v-model="showHidden" type="checkbox" />
              Show hidden
            </label>
            <div class="ws-picker__toolbar-actions">
              <button
                type="button"
                class="btn-ghost ws-picker__new-folder-btn"
                :disabled="browsePath === '/'"
                title="Create a new folder here"
                @click="createFolder"
              >
                + New Folder
              </button>
              <button
                type="button"
                class="btn-ghost ws-picker__add-folder-btn"
                :disabled="folders.includes(browsePath)"
                @click="addFolder(browsePath)"
              >
                + Add current folder
              </button>
            </div>
          </div>

          <div class="ws-picker__status">
            <span v-if="browseLoading">Loading...</span>
            <span v-else-if="browseError" style="color: #c44">{{ browseError }}</span>
          </div>

          <div class="ws-picker__list">
            <div class="ws-picker__filter-wrap">
              <input
                v-model="filter"
                class="field ws-picker__filter"
                type="search"
                placeholder="Filter..."
                autocomplete="off"
              />
            </div>
            <div>
              <button
                v-if="browsePath !== '/'"
                type="button"
                class="ws-entry--parent"
                @click="navigateUp"
              >
                <span>..</span>
              </button>
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
                  type="button"
                  class="btn-ghost ws-entry__rename"
                  title="Rename folder"
                  @click="renameEntry(entry)"
                >✎</button>
                <button
                  v-if="!folders.includes(entry.path)"
                  type="button"
                  class="btn-ghost ws-entry__add"
                  @click="addFolder(entry.path)"
                >
                  + Add
                </button>
                <span v-else class="ws-entry__added">added</span>
              </div>
              <div
                v-if="!browseLoading && filteredEntries().length === 0 && browsePath !== '/'"
                style="padding: 8px; font-size: 11px; color: var(--text-muted)"
              >
                {{ filter.trim() ? 'No matches.' : 'Empty.' }}
              </div>
            </div>
          </div>
        </div>

        <div class="ws-step__footer">
          <button
            v-if="existing.length > 0"
            type="button"
            class="btn btn-ghost"
            @click="backToList"
          >
            &larr; Back to list
          </button>
          <span class="ws-step__count">
            {{ folders.length }} {{ folders.length === 1 ? 'folder' : 'folders' }} added
          </span>
          <button
            type="button"
            class="btn btn-accent"
            :disabled="!canProceed"
            @click="goNext"
          >
            Next: Name &rarr;
          </button>
        </div>
      </div>

        <div v-show="step === 2" class="ws-picker__body ws-picker__body--step">
        <p class="ws-step__instruction">
          Name this workspace and review its folders. The name is stable across folder edits.
        </p>
        <div class="ws-picker__name-row">
          <label class="ws-picker__name-label" for="ws-name-input">Workspace name</label>
          <input
            id="ws-name-input"
            v-model="wsName"
            class="field ws-picker__name-input"
            type="text"
            :placeholder="basenames(folders) || 'My workspace'"
            autocomplete="off"
          />
        </div>
        <div class="ws-picker__selection ws-picker__selection--review">
          <div class="ws-picker__selection-header">
            <span class="ws-picker__selection-label">Folders</span>
            <button
              type="button"
              class="btn-ghost ws-picker__clear-btn"
              :disabled="folders.length === 0"
              @click="clearSelection"
            >
              Clear all
            </button>
          </div>
          <div class="ws-picker__selection-list">
            <div
              v-if="folders.length === 0"
              style="font-size: 11px; color: var(--text-muted); padding: 4px 2px"
            >
              No folders selected. Go back to step 1 to add some.
            </div>
            <div
              v-for="(f, i) in folders"
              :key="f"
              class="ws-selected-item"
            >
              <span class="ws-selected-item__path" :title="f">{{ shortenPath(f) }}</span>
              <button
                type="button"
                class="btn-ghost ws-selected-item__remove"
                @click="removeFolder(i)"
              >
                &times;
              </button>
            </div>
          </div>
          <div class="ws-picker__selection-footer ws-picker__selection-footer--review">
            <button type="button" class="btn btn-ghost" @click="goBack">
              &larr; Back
            </button>
            <div class="ws-step__footer-right">
              <span class="ws-picker__apply-status">{{ applyStatus }}</span>
              <button
                type="button"
                class="btn btn-accent"
                :disabled="saving || folders.length === 0"
                @click="confirm"
              >
                {{ saving ? 'Applying...' : 'Activate' }}
              </button>
            </div>
          </div>
        </div>
      </div>
      </template>
    </div>
  </div>
</template>

<style scoped>
.ws-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  overflow-y: auto;
  flex: 1;
}
.ws-list__item {
  display: flex;
  align-items: stretch;
  gap: 6px;
  border: 1px solid var(--border);
  border-radius: 8px;
  background: var(--bg-elevated);
  overflow: hidden;
}
.ws-list__item--active {
  border-color: var(--accent);
}
.ws-list__main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
  align-items: flex-start;
  text-align: left;
  background: none;
  border: none;
  cursor: pointer;
  padding: 8px 10px;
  color: var(--text);
}
.ws-list__main:hover:not(:disabled) {
  background: var(--bg-hover, rgba(127, 127, 127, 0.08));
}
.ws-list__name {
  font-size: 13px;
  font-weight: 600;
  line-height: 1.4;
  display: flex;
  align-items: center;
  gap: 6px;
}
.ws-list__paths {
  font-size: 11px;
  /* Without an explicit line-height the single-line box collapses to the font
   * size and overflow:hidden clips the descenders ("half visible" paths). */
  line-height: 1.6;
  color: var(--text-muted);
  font-family: monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}
.ws-list__badge {
  font-size: 9px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.4px;
  padding: 1px 5px;
  border-radius: 999px;
}
.ws-list__badge--active {
  background: var(--accent);
  color: var(--accent-contrast, #fff);
}
.ws-list__badge--dormant {
  background: #b8860b;
  color: #fff;
}
.ws-list__edit {
  flex-shrink: 0;
  font-size: 11px;
  padding: 0 12px;
  align-self: center;
}
/* The close button must be a rounded square so its focus ring isn't a sharp
 * rectangle (the outline follows border-radius). */
.ws-picker__close {
  font-size: 18px;
  line-height: 1;
  padding: 4px 9px;
  flex-shrink: 0;
  border-radius: 8px;
}
.ws-picker__name-row {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-bottom: 10px;
}
.ws-picker__name-label {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-muted);
  text-transform: uppercase;
  letter-spacing: 0.5px;
}
.ws-picker__name-input {
  width: 100%;
}
</style>
