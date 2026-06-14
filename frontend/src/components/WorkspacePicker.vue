<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import { api } from '../api/client';
import { useTaskStore } from '../stores/tasks';
import { useDialogStore } from '../stores/dialog';
import { useToastStore } from '../stores/toast';
import { useFocusTrap } from '../composables/useFocusTrap';

interface BrowseEntry {
  name: string;
  path: string;
  is_git_repo: boolean;
}

interface BrowseResponse {
  path: string;
  entries: BrowseEntry[];
}

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>();

const store = useTaskStore();
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
const workspaces = ref<string[]>([]);
const browsePath = ref('/');
const pathInput = ref('/');
const browseEntries = ref<BrowseEntry[]>([]);
const browseLoading = ref(false);
const browseError = ref('');
const filter = ref('');
const showHidden = ref(false);
const saving = ref(false);
const applyStatus = ref('');

// Two step wizard: 1 = choose folders, 2 = review and activate.
const step = ref(1);
const canProceed = computed(() => workspaces.value.length > 0);

function goNext() {
  if (!canProceed.value) return;
  step.value = 2;
}

function goBack() {
  step.value = 1;
}

watch(
  () => props.modelValue,
  async (open) => {
    if (!open) return;
    workspaces.value = [...(store.config?.workspaces ?? [])];
    browsePath.value = '/';
    pathInput.value = '/';
    browseError.value = '';
    filter.value = '';
    applyStatus.value = '';
    step.value = 1;
    await browse('/');
    document.addEventListener('keydown', onKey);
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

async function browse(path: string) {
  browseLoading.value = true;
  browseError.value = '';
  try {
    const res = await api<BrowseResponse>(
      'GET',
      `/api/workspaces/browse?path=${encodeURIComponent(path)}`,
    );
    browsePath.value = res.path;
    pathInput.value = res.path;
    browseEntries.value = res.entries;
  } catch (e: unknown) {
    browseError.value = e instanceof Error ? e.message : 'Failed to browse directory';
    browseEntries.value = [];
  } finally {
    browseLoading.value = false;
  }
}

function navigateUp() {
  const parent = browsePath.value.split('/').slice(0, -1).join('/') || '/';
  browse(parent);
}

function navigateInto(entry: BrowseEntry) {
  browse(entry.path);
}

function goToPath() {
  if (pathInput.value.trim()) browse(pathInput.value.trim());
}

function onPathKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    e.preventDefault();
    goToPath();
  }
}

function addWorkspace(path: string) {
  if (!workspaces.value.includes(path)) {
    workspaces.value.push(path);
  }
}

function removeWorkspace(index: number) {
  workspaces.value.splice(index, 1);
}

function clearSelection() {
  workspaces.value = [];
}

async function save() {
  saving.value = true;
  applyStatus.value = 'Applying...';
  try {
    await api('PUT', '/api/workspaces', { workspaces: workspaces.value });
    await store.fetchConfig();
    applyStatus.value = '';
    close();
  } catch (e) {
    console.error('save workspaces:', e);
    applyStatus.value = e instanceof Error ? e.message : 'Failed to apply';
  } finally {
    saving.value = false;
  }
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
  return entries;
}

// Collapse a home directory prefix to '~' for display (full path stays in title).
function shortenPath(path: string) {
  const m = path.match(/^(\/(?:Users|home)\/[^/]+|[A-Z]:\\Users\\[^\\]+)/);
  if (m) return '~' + path.substring(m[1].length);
  return path;
}

function breadcrumbSegments() {
  const parts = browsePath.value.split('/').filter(Boolean);
  const segs: { label: string; path: string }[] = [{ label: '/', path: '/' }];
  let acc = '';
  for (const p of parts) {
    acc += `/${p}`;
    segs.push({ label: p, path: acc });
  }
  return segs;
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
          <h3 class="ws-picker__title">Select Workspaces</h3>
        </div>
        <button
          v-if="dismissable"
          type="button"
          class="btn-ghost"
          style="font-size: 18px; padding: 2px 8px; flex-shrink: 0"
          @click="close"
        >
          &times;
        </button>
      </div>

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
          <span class="ws-step__label">Review &amp; activate</span>
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
              :disabled="workspaces.includes(browsePath)"
              @click="addWorkspace(browsePath)"
            >
              + Add current folder
            </button>
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
                  v-if="!workspaces.includes(entry.path)"
                  type="button"
                  class="btn-ghost ws-entry__add"
                  @click="addWorkspace(entry.path)"
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
          <span class="ws-step__count">
            {{ workspaces.length }} {{ workspaces.length === 1 ? 'folder' : 'folders' }} added
          </span>
          <button
            type="button"
            class="btn btn-accent"
            :disabled="!canProceed"
            @click="goNext"
          >
            Next: Review &rarr;
          </button>
        </div>
      </div>

      <div v-show="step === 2" class="ws-picker__body ws-picker__body--step">
        <p class="ws-step__instruction">
          These folders become task boards. Remove any you do not want, then activate.
        </p>
        <div class="ws-picker__selection ws-picker__selection--review">
          <div class="ws-picker__selection-header">
            <span class="ws-picker__selection-label">Selected</span>
            <button
              type="button"
              class="btn-ghost ws-picker__clear-btn"
              :disabled="workspaces.length === 0"
              @click="clearSelection"
            >
              Clear all
            </button>
          </div>
          <div class="ws-picker__selection-list">
            <div
              v-if="workspaces.length === 0"
              style="font-size: 11px; color: var(--text-muted); padding: 4px 2px"
            >
              No folders selected. Go back to step 1 to add some.
            </div>
            <div
              v-for="(ws, i) in workspaces"
              :key="ws"
              class="ws-selected-item"
            >
              <span class="ws-selected-item__path" :title="ws">{{ shortenPath(ws) }}</span>
              <button
                type="button"
                class="btn-ghost ws-selected-item__remove"
                @click="removeWorkspace(i)"
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
                :disabled="saving"
                @click="save"
              >
                {{ saving ? 'Applying...' : 'Activate' }}
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
