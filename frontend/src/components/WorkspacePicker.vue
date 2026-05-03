<script setup lang="ts">
import { onBeforeUnmount, ref, watch } from 'vue';
import { api } from '../api/client';
import { useTaskStore } from '../stores/tasks';

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

function onBackdrop(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('modal-overlay')) close();
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') close();
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
    <div class="modal-card ws-picker" role="dialog" aria-label="Workspace Picker">
      <div class="ws-picker__header">
        <div style="flex: 1; min-width: 0">
          <h3 class="ws-picker__title">Select Workspaces</h3>
          <p class="ws-picker__subtitle">
            Choose directories to activate a task board.
          </p>
        </div>
        <button
          type="button"
          class="btn-ghost"
          style="font-size: 18px; padding: 2px 8px; flex-shrink: 0"
          @click="close"
        >
          &times;
        </button>
      </div>

      <div class="ws-picker__body">
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
              <button
                type="button"
                class="btn-ghost"
                style="padding: 1px 4px; font-size: 11px"
                @click="browse(seg.path)"
              >
                {{ seg.label }}
              </button>
              <span v-if="i < breadcrumbSegments().length - 1" style="opacity: 0.5">/</span>
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
                <span>&#x21B0;</span> ..
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
                  <span>&#x1F4C1;</span>
                  <span style="overflow: hidden; text-overflow: ellipsis">{{ entry.name }}</span>
                  <span v-if="entry.is_git_repo" class="ws-entry__badge">git</span>
                </button>
                <button
                  v-if="!workspaces.includes(entry.path)"
                  type="button"
                  class="btn-ghost ws-entry__add"
                  @click="addWorkspace(entry.path)"
                >
                  Add
                </button>
                <span v-else class="ws-entry__added">Added</span>
              </div>
              <div
                v-if="!browseLoading && filteredEntries().length === 0 && browsePath !== '/'"
                style="padding: 8px; font-size: 11px; color: var(--text-muted)"
              >
                No subdirectories.
              </div>
            </div>
          </div>
        </div>

        <div class="ws-picker__selection">
          <div class="ws-picker__selection-header">
            <span class="ws-picker__selection-label">Selected</span>
            <button
              type="button"
              class="btn-ghost ws-picker__clear-btn"
              :disabled="workspaces.length === 0"
              @click="clearSelection"
            >
              Clear
            </button>
          </div>
          <div class="ws-picker__selection-list">
            <div
              v-if="workspaces.length === 0"
              style="font-size: 11px; color: var(--text-muted); padding: 4px 2px"
            >
              No workspaces selected.
            </div>
            <div
              v-for="(ws, i) in workspaces"
              :key="ws"
              class="ws-selected-item"
            >
              <span class="ws-selected-item__path" :title="ws">{{ ws }}</span>
              <button
                type="button"
                class="btn-ghost ws-selected-item__remove"
                @click="removeWorkspace(i)"
              >
                Remove
              </button>
            </div>
          </div>
          <div class="ws-picker__selection-footer">
            <button
              type="button"
              class="btn btn-accent"
              :disabled="saving"
              @click="save"
            >
              {{ saving ? 'Applying...' : 'Apply' }}
            </button>
            <span class="ws-picker__apply-status">{{ applyStatus }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
