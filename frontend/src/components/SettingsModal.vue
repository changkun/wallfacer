<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, computed } from 'vue';
import { api } from '../api/client';
import { useTaskStore } from '../stores/tasks';
import { useTheme } from '../composables/useTheme';
import type { Task } from '../api/types';

const emit = defineEmits<{ close: []; workspaces: [] }>();
const store = useTaskStore();
const { theme } = useTheme();

type TabKey = 'appearance' | 'execution' | 'sandbox' | 'workspace' | 'trash' | 'about';
const activeTab = ref<TabKey>('appearance');
const saving = ref(false);

const autopilot = ref(false);
const autotest = ref(false);
const autosubmit = ref(false);
const autosync = ref(false);
const autopush = ref(false);

// Sandbox tab state
const defaultSandbox = ref('');
const savingSandbox = ref(false);
const sandboxStatus = ref('');
const sandboxes = computed(() => store.config?.sandboxes ?? []);

// Workspace tab state
const workspaces = computed(() => store.config?.workspaces ?? []);

// Trash tab state
const deletedTasks = ref<Task[]>([]);
const trashLoading = ref(false);
const trashError = ref('');
const restoring = ref<Record<string, boolean>>({});

onMounted(() => {
  syncFromConfig();
  document.addEventListener('keydown', onKey);
});
onUnmounted(() => document.removeEventListener('keydown', onKey));

function syncFromConfig() {
  if (!store.config) return;
  autopilot.value = store.config.autopilot;
  autotest.value = store.config.autotest;
  autosubmit.value = store.config.autosubmit;
  autosync.value = store.config.autosync;
  autopush.value = store.config.autopush;
  defaultSandbox.value = store.config.default_sandbox || '';
}

watch(() => store.config, syncFromConfig);

watch(activeTab, (tab) => {
  if (tab === 'trash') {
    void loadDeletedTasks();
  }
});

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close');
}

async function saveExecution() {
  saving.value = true;
  try {
    await api('PUT', '/api/config', {
      autopilot: autopilot.value,
      autotest: autotest.value,
      autosubmit: autosubmit.value,
      autosync: autosync.value,
      autopush: autopush.value,
    });
    await store.fetchConfig();
  } catch (e) {
    console.error('save config:', e);
  } finally {
    saving.value = false;
  }
}

function onOverlayClick(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('modal-overlay')) emit('close');
}

function setTheme(mode: 'light' | 'dark' | 'auto') {
  theme.value = mode;
}

async function saveDefaultSandbox(value: string) {
  defaultSandbox.value = value;
  savingSandbox.value = true;
  sandboxStatus.value = '';
  try {
    await api('PUT', '/api/config', { default_sandbox: value });
    await store.fetchConfig();
    sandboxStatus.value = 'Saved';
    setTimeout(() => { sandboxStatus.value = ''; }, 2000);
  } catch (e) {
    console.error('save default sandbox:', e);
    sandboxStatus.value = 'Failed to save';
  } finally {
    savingSandbox.value = false;
  }
}

function openWorkspacePicker() {
  emit('workspaces');
}

async function loadDeletedTasks() {
  trashLoading.value = true;
  trashError.value = '';
  try {
    deletedTasks.value = await api<Task[]>('GET', '/api/tasks/deleted');
  } catch (e) {
    console.error('load deleted tasks:', e);
    trashError.value = e instanceof Error ? e.message : 'Failed to load';
  } finally {
    trashLoading.value = false;
  }
}

async function restoreTask(id: string) {
  restoring.value = { ...restoring.value, [id]: true };
  try {
    await api('POST', `/api/tasks/${id}/restore`);
    deletedTasks.value = deletedTasks.value.filter((t) => t.id !== id);
    await store.fetchTasks();
  } catch (e) {
    console.error('restore task:', e);
    trashError.value = e instanceof Error ? e.message : 'Failed to restore';
  } finally {
    const next = { ...restoring.value };
    delete next[id];
    restoring.value = next;
  }
}

function formatDate(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

const tabs: { key: TabKey; label: string }[] = [
  { key: 'appearance', label: 'Appearance' },
  { key: 'execution', label: 'Execution' },
  { key: 'sandbox', label: 'Sandbox' },
  { key: 'workspace', label: 'Workspace' },
  { key: 'trash', label: 'Trash' },
  { key: 'about', label: 'About' },
];
</script>

<template>
  <div class="modal-overlay fixed inset-0 z-50 flex items-center justify-center p-4" @click="onOverlayClick">
    <div class="modal-card settings-modal-card" style="max-width: 840px; width: 100%">
      <div class="p-6 settings-modal-content">
        <div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 12px">
          <h3 style="font-size: 16px; font-weight: 600; margin: 0">Settings</h3>
          <button
            type="button"
            @click="emit('close')"
            style="background: none; border: none; cursor: pointer; font-size: 20px; color: var(--text-muted); line-height: 1"
            aria-label="Close settings"
          >&times;</button>
        </div>

        <div class="settings-layout">
          <div class="settings-tab-list" role="tablist" aria-label="Settings tabs">
            <button
              v-for="tab in tabs"
              :key="tab.key"
              type="button"
              class="settings-tab"
              :class="{ active: activeTab === tab.key }"
              role="tab"
              :aria-selected="activeTab === tab.key"
              @click="activeTab = tab.key"
            >{{ tab.label }}</button>
          </div>

          <div class="settings-tab-content-wrap">
            <div
              v-show="activeTab === 'appearance'"
              class="settings-tab-content"
              :class="{ active: activeTab === 'appearance' }"
              data-settings-tab="appearance"
            >
              <div class="settings-card">
                <div class="settings-card-head">
                  <h4>Theme</h4>
                  <p>Choose the interface color mode for the current session.</p>
                </div>
                <div class="theme-switch settings-theme-switch" role="group" aria-label="Theme mode">
                  <button
                    type="button"
                    data-mode="light"
                    :class="{ active: theme === 'light' }"
                    @click="setTheme('light')"
                  >Light</button>
                  <button
                    type="button"
                    data-mode="dark"
                    :class="{ active: theme === 'dark' }"
                    @click="setTheme('dark')"
                  >Dark</button>
                  <button
                    type="button"
                    data-mode="auto"
                    :class="{ active: theme === 'auto' }"
                    @click="setTheme('auto')"
                  >Auto</button>
                </div>
              </div>
            </div>

            <div
              v-show="activeTab === 'execution'"
              class="settings-tab-content"
              :class="{ active: activeTab === 'execution' }"
              data-settings-tab="execution"
            >
              <div class="settings-card">
                <div class="settings-card-head">
                  <h4>Automation</h4>
                  <p>Toggle which automation steps run for new tasks.</p>
                </div>
                <label class="settings-toggle">
                  <input type="checkbox" v-model="autopilot" />
                  Autopilot (auto-promote backlog tasks to in-progress)
                </label>
                <label class="settings-toggle">
                  <input type="checkbox" v-model="autotest" />
                  Auto-test (run test agent after implementation)
                </label>
                <label class="settings-toggle">
                  <input type="checkbox" v-model="autosubmit" />
                  Auto-submit (auto-commit when agent finishes)
                </label>
                <label class="settings-toggle">
                  <input type="checkbox" v-model="autosync" />
                  Auto-sync (rebase worktrees on main before execution)
                </label>
                <label class="settings-toggle">
                  <input type="checkbox" v-model="autopush" />
                  Auto-push (push after commit pipeline completes)
                </label>
                <div style="margin-top: 10px">
                  <button
                    type="button"
                    class="btn-icon"
                    style="font-size: 12px; padding: 4px 12px"
                    :disabled="saving"
                    @click="saveExecution"
                  >{{ saving ? 'Saving...' : 'Save' }}</button>
                </div>
              </div>
            </div>

            <div
              v-show="activeTab === 'sandbox'"
              class="settings-tab-content"
              :class="{ active: activeTab === 'sandbox' }"
              data-settings-tab="sandbox"
            >
              <div class="settings-card">
                <div class="settings-card-head">
                  <h4>Sandbox</h4>
                  <p>Choose the default sandbox runtime used for new tasks.</p>
                </div>
                <div v-if="sandboxes.length === 0" style="font-size: 12px; color: var(--text-muted)">
                  No sandboxes are configured.
                </div>
                <div v-else class="settings-list">
                  <div
                    v-for="name in sandboxes"
                    :key="name"
                    class="settings-row"
                    style="display: flex; align-items: center; justify-content: space-between; gap: 8px"
                  >
                    <div style="display: flex; align-items: center; gap: 8px">
                      <span style="font-size: 12px; font-weight: 600">{{ name }}</span>
                      <span
                        v-if="name === defaultSandbox"
                        style="font-size: 10px; padding: 2px 6px; border-radius: 999px; background: var(--tint-accent, var(--bg-secondary)); color: var(--accent); font-weight: 600; text-transform: uppercase; letter-spacing: 0.5px"
                      >Default</span>
                    </div>
                    <button
                      v-if="name !== defaultSandbox"
                      type="button"
                      class="btn-icon"
                      style="font-size: 12px; padding: 4px 10px"
                      :disabled="savingSandbox"
                      @click="saveDefaultSandbox(name)"
                    >Set as default</button>
                  </div>
                </div>
                <div
                  v-if="sandboxStatus"
                  style="margin-top: 8px; font-size: 11px; color: var(--text-muted)"
                >{{ sandboxStatus }}</div>
              </div>
            </div>

            <div
              v-show="activeTab === 'workspace'"
              class="settings-tab-content"
              :class="{ active: activeTab === 'workspace' }"
              data-settings-tab="workspace"
            >
              <div class="settings-card">
                <div class="settings-card-head">
                  <h4>Active Workspaces</h4>
                  <p>Workspaces mounted into every task container for the current group.</p>
                </div>
                <div v-if="workspaces.length === 0" style="font-size: 12px; color: var(--text-muted)">
                  No workspaces selected.
                </div>
                <div v-else class="settings-list">
                  <div
                    v-for="path in workspaces"
                    :key="path"
                    class="settings-row"
                    style="font-family: monospace; font-size: 12px; color: var(--text-secondary); word-break: break-all"
                  >{{ path }}</div>
                </div>
                <div style="margin-top: 12px">
                  <button
                    type="button"
                    class="btn-icon"
                    style="font-size: 12px; padding: 4px 12px"
                    @click="openWorkspacePicker"
                  >Change workspaces...</button>
                </div>
              </div>
            </div>

            <div
              v-show="activeTab === 'trash'"
              class="settings-tab-content"
              :class="{ active: activeTab === 'trash' }"
              data-settings-tab="trash"
            >
              <div class="settings-card">
                <div class="settings-card-head">
                  <h4>Deleted Tasks</h4>
                  <p>Soft-deleted tasks remain recoverable for 7 days.</p>
                </div>
                <div
                  v-if="trashError"
                  role="alert"
                  style="margin-bottom: 10px; padding: 8px 10px; border-radius: 6px; background: var(--tint-danger, #fde7e7); color: var(--danger, #a02929); font-size: 12px"
                >{{ trashError }}</div>
                <div
                  v-if="trashLoading"
                  style="font-size: 12px; color: var(--text-muted)"
                >Loading deleted tasks...</div>
                <div
                  v-else-if="deletedTasks.length === 0"
                  style="font-size: 12px; color: var(--text-muted)"
                >Trash is empty</div>
                <div v-else class="settings-list" role="list">
                  <div
                    v-for="task in deletedTasks"
                    :key="task.id"
                    class="settings-row"
                    role="listitem"
                    style="display: flex; align-items: center; justify-content: space-between; gap: 12px"
                  >
                    <div style="min-width: 0; flex: 1">
                      <div style="font-size: 12px; font-weight: 600; color: var(--text-primary); overflow: hidden; text-overflow: ellipsis; white-space: nowrap">
                        {{ task.title || task.prompt || task.id }}
                      </div>
                      <div style="font-size: 11px; color: var(--text-muted); margin-top: 2px">
                        {{ task.status }} &middot; updated {{ formatDate(task.updated_at) }}
                      </div>
                    </div>
                    <button
                      type="button"
                      class="btn-icon"
                      style="font-size: 12px; padding: 4px 10px"
                      :disabled="!!restoring[task.id]"
                      @click="restoreTask(task.id)"
                    >{{ restoring[task.id] ? 'Restoring...' : 'Restore' }}</button>
                  </div>
                </div>
              </div>
            </div>

            <div
              v-show="activeTab === 'about'"
              class="settings-tab-content"
              :class="{ active: activeTab === 'about' }"
              data-settings-tab="about"
            >
              <div style="margin-bottom: 10px; font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px">
                About
              </div>
              <div style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px">
                <div style="width: 32px; height: 32px; border-radius: 7px; background: linear-gradient(135deg, #d97757 0%, #c4623f 60%, #a84e2e 100%); display: flex; align-items: center; justify-content: center; flex-shrink: 0">
                  <span style="color: white; font-size: 15px; font-family: 'Instrument Serif', Georgia, serif; font-style: italic; font-weight: 400; line-height: 1">W</span>
                </div>
                <div>
                  <a
                    href="https://github.com/changkun/wallfacer"
                    target="_blank"
                    rel="noopener noreferrer"
                    style="font-size: 13px; font-weight: 400; font-family: 'Instrument Serif', Georgia, serif; font-style: italic; background: linear-gradient(135deg, #d97757 0%, #c4623f 60%, #a84e2e 100%); -webkit-background-clip: text; -webkit-text-fill-color: transparent; background-clip: text; line-height: 1.3; text-decoration: none; display: block"
                  >Wallfacer</a>
                  <div style="font-size: 11px; color: var(--text-muted); margin-top: 2px">
                    Dispatch AI agents. Collect merged code.
                  </div>
                </div>
              </div>
              <div style="display: flex; flex-direction: column; gap: 5px; font-size: 11px; color: var(--text-muted)">
                <a
                  href="https://github.com/changkun/wallfacer"
                  target="_blank"
                  rel="noopener noreferrer"
                  style="display: inline-flex; align-items: center; gap: 6px; color: var(--text-muted); text-decoration: none"
                >
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor" style="flex-shrink: 0; opacity: 0.7">
                    <path d="M12 0C5.374 0 0 5.373 0 12c0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576C20.566 21.797 24 17.3 24 12c0-6.627-5.373-12-12-12z" />
                  </svg>
                  github.com/changkun/wallfacer
                </a>
                <div style="display: inline-flex; align-items: center; gap: 6px">
                  <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="flex-shrink: 0; opacity: 0.7">
                    <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
                  </svg>
                  MIT License &middot; Copyright &copy; 2026
                  <a
                    href="https://changkun.de"
                    target="_blank"
                    rel="noopener noreferrer"
                    style="color: inherit; text-decoration: none"
                  >Changkun Ou</a>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
