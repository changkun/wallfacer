<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import { api } from '../api/client';
import { useTaskStore } from '../stores/tasks';
import { useWorkspacesStore } from '../stores/workspaces';
import { useUiStore } from '../stores/ui';
import { useDialogStore } from '../stores/dialog';
import { useToastStore } from '../stores/toast';
import { useFocusTrap } from '../composables/useFocusTrap';
import { useFolderBrowser } from '../composables/useFolderBrowser';
import FolderBrowser from './FolderBrowser.vue';
import { workspaceLabel } from '../lib/workspaceLabel';
import type { Workspace } from '../api/types';

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();

const store = useTaskStore();
const wsStore = useWorkspacesStore();
const ui = useUiStore();
const dialog = useDialogStore();
const toast = useToastStore();

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
const browser = useFolderBrowser();
const { browsePath, pathInput, browseError, filter, browse, shortenPath } = browser;
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

// Open the host OS native folder chooser and add the picked folder to the list.
// When no native picker is available (501: cloud/headless/unknown platform) we
// fall back to the in-app directory browser shown below.
async function pickFolderNative() {
  try {
    const res = await api<{ path?: string; cancelled?: boolean }>('POST', '/api/workspaces/pick-folder');
    if (res.path) addFolder(res.path);
  } catch {
    toast.push('Native folder picker unavailable here — browse below instead.', { kind: 'info' });
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

</script>

<template>
  <div
    v-if="modelValue"
    class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4"
    @click="onBackdrop"
  >
    <div ref="cardRef" class="dialog dialog--wide ws-picker" role="dialog" aria-modal="true" aria-label="Workspace Picker">
      <div class="dialog-head ws-picker__header">
        <div class="dialog-head__main">
          <h3 class="dialog-title ws-picker__title">
            {{ view === 'list' ? 'Select workspace' : editingId ? 'Edit workspace' : 'New workspace' }}
          </h3>
          <p class="dialog-sub">
            <template v-if="view === 'list'">Click a workspace to switch the board to it. Editing folders never loses history.</template>
            <template v-else-if="step === 1">Pick the project folders this workspace spans. Each one appears in the list below.</template>
            <template v-else>Name this workspace and review its folders. The name is stable across folder edits.</template>
          </p>
        </div>
        <button
          v-if="dismissable"
          type="button"
          class="icon-btn ws-picker__close"
          aria-label="Close"
          @click="close"
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><line x1="6" y1="6" x2="18" y2="18"></line><line x1="18" y1="6" x2="6" y2="18"></line></svg>
        </button>
      </div>

      <!-- List view: pick an existing workspace to activate. -->
      <template v-if="view === 'list'">
        <div class="dialog-body ws-picker__body ws-picker__list-view">
          <div class="card compact">
            <div class="rows ws-list">
              <div
                v-for="ws in existing"
                :key="ws.id"
                class="row ws-list__item"
                :class="{ 'ws-list__item--active': wsStore.isActive(ws.id) }"
              >
                <button
                  type="button"
                  class="row-main ws-list__main"
                  :disabled="activatingId !== null"
                  @click="activateExisting(ws)"
                >
                  <span class="row-title ws-list__name">
                    {{ workspaceLabel(ws.name, ws.folders) }}
                    <span v-if="wsStore.isActive(ws.id)" class="pill pill-brand ws-list__badge ws-list__badge--active">active</span>
                    <span v-if="ws.dormant" class="pill pill-warn ws-list__badge ws-list__badge--dormant">recovered</span>
                  </span>
                  <span class="row-meta ws-list__paths" :title="ws.folders.join('\n')">
                    {{ ws.folders.length ? ws.folders.map(shortenPath).join('  ·  ') : 'No folders. Re-point to use.' }}
                  </span>
                </button>
                <div class="row-end">
                  <button
                    type="button"
                    class="btn sm ghost ws-list__edit"
                    title="Edit name, folders, and limits"
                    @click="close(); ui.openWorkspaceEdit(ws.id)"
                  >Edit</button>
                </div>
              </div>
            </div>
          </div>
        </div>
        <div class="dialog-foot ws-step__footer">
          <span class="dialog-foot__status ws-picker__apply-status">{{ applyStatus }}</span>
          <button type="button" class="btn" @click="enterNewWorkspace">
            + New workspace
          </button>
        </div>
      </template>

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

        <div v-show="step === 1" class="dialog-body ws-picker__body ws-picker__body--step">
          <div class="ws-picker__pick-row">
            <button type="button" class="btn ws-picker__pick-btn" @click="pickFolderNative">
              Choose folder…
            </button>
            <span class="ws-picker__pick-hint">Opens the system file browser, or browse below.</span>
          </div>
          <FolderBrowser
            class="ws-picker__browser"
            :browser="browser"
            :added="folders"
            renamable
            @add="addFolder"
            @rename="renameEntry"
          />
        </div>
        <div v-show="step === 1" class="dialog-foot ws-step__footer">
          <button
            v-if="existing.length > 0"
            type="button"
            class="btn ghost"
            @click="backToList"
          >
            &larr; Back to list
          </button>
          <span class="dialog-foot__status ws-step__count">
            {{ folders.length }} {{ folders.length === 1 ? 'folder' : 'folders' }} added
          </span>
          <button
            type="button"
            class="btn"
            :disabled="!canProceed"
            @click="goNext"
          >
            Next: Name &rarr;
          </button>
        </div>

        <div v-show="step === 2" class="dialog-body ws-picker__body ws-picker__body--step">
          <div class="ws-picker__name-row">
            <label class="eyebrow ws-picker__name-label" for="ws-name-input">Workspace name</label>
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
              <span class="eyebrow ws-picker__selection-label">Folders</span>
              <button
                type="button"
                class="btn sm ghost ws-picker__clear-btn"
                :disabled="folders.length === 0"
                @click="clearSelection"
              >
                Clear all
              </button>
            </div>
            <div class="ws-picker__selection-list">
              <div v-if="folders.length === 0" class="ws-picker__empty">
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
                  class="icon-btn sm ws-selected-item__remove"
                  aria-label="Remove folder"
                  @click="removeFolder(i)"
                >
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" aria-hidden="true"><line x1="6" y1="6" x2="18" y2="18"></line><line x1="18" y1="6" x2="6" y2="18"></line></svg>
                </button>
              </div>
            </div>
          </div>
        </div>
        <div v-show="step === 2" class="dialog-foot ws-picker__selection-footer ws-picker__selection-footer--review">
          <button type="button" class="btn ghost" @click="goBack">
            &larr; Back
          </button>
          <span class="dialog-foot__status ws-picker__apply-status">{{ applyStatus }}</span>
          <button
            type="button"
            class="btn"
            :disabled="saving || folders.length === 0"
            @click="confirm"
          >
            {{ saving ? 'Applying...' : 'Activate' }}
          </button>
        </div>
      </template>
    </div>
  </div>
</template>
