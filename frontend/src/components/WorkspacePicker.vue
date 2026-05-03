<script setup lang="ts">
import { ref, watch } from 'vue';
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
const browseEntries = ref<BrowseEntry[]>([]);
const browseLoading = ref(false);
const browseError = ref('');
const saving = ref(false);

watch(() => props.modelValue, async (open) => {
  if (!open) return;
  workspaces.value = [...(store.config?.workspaces ?? [])];
  browsePath.value = '/';
  browseError.value = '';
  await browse('/');
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

function addWorkspace(path: string) {
  if (!workspaces.value.includes(path)) {
    workspaces.value.push(path);
  }
}

function removeWorkspace(index: number) {
  workspaces.value.splice(index, 1);
}

async function save() {
  saving.value = true;
  try {
    await api('PUT', '/api/workspaces', { workspaces: workspaces.value });
    await store.fetchConfig();
    close();
  } catch (e) {
    console.error('save workspaces:', e);
  } finally {
    saving.value = false;
  }
}

function close() {
  emit('update:modelValue', false);
}

function onBackdrop(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('modal-backdrop')) close();
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') close();
}
</script>

<template>
  <div v-if="modelValue" class="modal-backdrop" @click="onBackdrop" @keydown="onKey">
    <div class="modal" role="dialog" aria-label="Workspace Picker">
      <header class="modal-header">
        <h2>Workspaces</h2>
        <button class="modal-close" @click="close">&times;</button>
      </header>

      <div class="modal-content">
        <!-- Current workspaces -->
        <section class="section">
          <h3 class="section-title">Current workspaces</h3>
          <div v-if="workspaces.length === 0" class="empty-msg">No workspaces selected.</div>
          <ul v-else class="ws-list">
            <li v-for="(ws, i) in workspaces" :key="ws" class="ws-item">
              <span class="ws-path" :title="ws">{{ ws }}</span>
              <button class="remove-btn" @click="removeWorkspace(i)" title="Remove">&times;</button>
            </li>
          </ul>
        </section>

        <!-- Browse directories -->
        <section class="section">
          <h3 class="section-title">Browse</h3>
          <div class="browse-bar">
            <button
              class="up-btn"
              :disabled="browsePath === '/'"
              @click="navigateUp"
              title="Go up"
            >
              ..
            </button>
            <span class="browse-path" :title="browsePath">{{ browsePath }}</span>
            <button
              class="add-current-btn"
              @click="addWorkspace(browsePath)"
              :disabled="workspaces.includes(browsePath)"
              title="Add this directory"
            >
              Add
            </button>
          </div>

          <div v-if="browseError" class="browse-error">{{ browseError }}</div>

          <div class="browse-list-wrap">
            <div v-if="browseLoading" class="browse-loading">Loading...</div>
            <ul v-else-if="browseEntries.length > 0" class="browse-list">
              <li
                v-for="entry in browseEntries"
                :key="entry.path"
                class="browse-item"
              >
                <button class="browse-name" @click="navigateInto(entry)">
                  <span class="folder-icon">&#x1F4C1;</span>
                  {{ entry.name }}
                  <span v-if="entry.is_git_repo" class="git-badge">git</span>
                </button>
                <button
                  class="add-btn"
                  @click="addWorkspace(entry.path)"
                  :disabled="workspaces.includes(entry.path)"
                >
                  Add
                </button>
              </li>
            </ul>
            <div v-else class="empty-msg">No subdirectories.</div>
          </div>
        </section>
      </div>

      <footer class="modal-footer">
        <button class="cancel-btn" @click="close">Cancel</button>
        <button class="save-btn" @click="save" :disabled="saving">
          {{ saving ? 'Saving...' : 'Save' }}
        </button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.35);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 200;
}
.modal {
  width: 520px;
  max-width: 90vw;
  max-height: 80vh;
  background: var(--bg);
  border: 1px solid var(--rule);
  border-radius: var(--r-lg, 10px);
  box-shadow: var(--sh-pop, 0 12px 40px rgba(0, 0, 0, 0.18));
  display: flex;
  flex-direction: column;
  overflow: hidden;
  font-family: var(--font-sans);
}
.modal-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  border-bottom: 1px solid var(--rule);
}
.modal-header h2 {
  margin: 0;
  font-size: 15px;
  font-weight: 600;
  color: var(--ink);
}
.modal-close {
  background: none;
  border: none;
  font-size: 20px;
  color: var(--ink-3);
  cursor: pointer;
}
.modal-close:hover {
  color: var(--ink);
}

.modal-content {
  flex: 1;
  overflow-y: auto;
  padding: 16px 20px;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.section-title {
  margin: 0 0 8px 0;
  font-size: 12px;
  font-weight: 600;
  color: var(--ink-2);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}

.empty-msg {
  font-size: 12px;
  color: var(--ink-4);
  padding: 8px 0;
}

/* Current workspaces list */
.ws-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.ws-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
}
.ws-path {
  flex: 1;
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--ink);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.remove-btn {
  flex-shrink: 0;
  background: none;
  border: none;
  color: var(--ink-3);
  font-size: 16px;
  cursor: pointer;
  padding: 0 4px;
  line-height: 1;
}
.remove-btn:hover {
  color: var(--ink);
}

/* Browse section */
.browse-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  background: var(--bg-sunk);
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  margin-bottom: 8px;
}
.browse-path {
  flex: 1;
  font-size: 12px;
  font-family: var(--font-mono);
  color: var(--ink-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.up-btn {
  flex-shrink: 0;
  padding: 2px 8px;
  font-size: 12px;
  font-weight: 600;
  font-family: var(--font-mono);
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  color: var(--ink-2);
  cursor: pointer;
}
.up-btn:hover:not(:disabled) {
  background: var(--bg-hover);
  color: var(--ink);
}
.up-btn:disabled {
  opacity: 0.3;
  cursor: default;
}
.add-current-btn {
  flex-shrink: 0;
  padding: 2px 10px;
  font-size: 11px;
  font-weight: 600;
  background: var(--accent);
  color: #fff;
  border: none;
  border-radius: var(--r-sm);
  cursor: pointer;
}
.add-current-btn:hover:not(:disabled) {
  background: var(--accent-2);
}
.add-current-btn:disabled {
  opacity: 0.35;
  cursor: default;
}

.browse-error {
  font-size: 12px;
  color: #c44;
  padding: 4px 0;
}
.browse-loading {
  font-size: 12px;
  color: var(--ink-3);
  padding: 12px 0;
  text-align: center;
}

.browse-list-wrap {
  max-height: 240px;
  overflow-y: auto;
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  background: var(--bg-card);
}
.browse-list {
  list-style: none;
  margin: 0;
  padding: 0;
}
.browse-item {
  display: flex;
  align-items: center;
  gap: 4px;
  border-bottom: 1px solid var(--rule);
}
.browse-item:last-child {
  border-bottom: none;
}
.browse-name {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  background: none;
  border: none;
  font-size: 12px;
  color: var(--ink);
  cursor: pointer;
  text-align: left;
  font-family: var(--font-sans);
}
.browse-name:hover {
  background: var(--bg-hover);
}
.folder-icon {
  font-size: 13px;
  flex-shrink: 0;
}
.git-badge {
  font-size: 10px;
  font-weight: 600;
  color: var(--accent);
  background: var(--bg-sunk);
  padding: 1px 5px;
  border-radius: var(--r-sm);
  border: 1px solid var(--rule);
}
.add-btn {
  flex-shrink: 0;
  margin-right: 8px;
  padding: 2px 10px;
  font-size: 11px;
  font-weight: 600;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  color: var(--ink-2);
  cursor: pointer;
}
.add-btn:hover:not(:disabled) {
  background: var(--bg-hover);
  color: var(--ink);
}
.add-btn:disabled {
  opacity: 0.3;
  cursor: default;
}

/* Footer */
.modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  padding: 12px 20px;
  border-top: 1px solid var(--rule);
}
.cancel-btn {
  padding: 5px 16px;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  font-size: 12px;
  color: var(--ink-2);
  cursor: pointer;
}
.cancel-btn:hover {
  background: var(--bg-hover);
  color: var(--ink);
}
.save-btn {
  padding: 5px 16px;
  background: var(--accent);
  color: #fff;
  border: none;
  border-radius: var(--r-sm);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}
.save-btn:hover {
  background: var(--accent-2);
}
.save-btn:disabled {
  opacity: 0.4;
  cursor: default;
}
</style>
