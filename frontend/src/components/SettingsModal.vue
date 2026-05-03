<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue';
import { api } from '../api/client';
import { useTaskStore } from '../stores/tasks';
import { useTheme } from '../composables/useTheme';

const emit = defineEmits<{ close: [] }>();
const store = useTaskStore();
const { theme } = useTheme();

type TabKey = 'appearance' | 'execution' | 'about';
const activeTab = ref<TabKey>('appearance');
const saving = ref(false);

const autopilot = ref(false);
const autotest = ref(false);
const autosubmit = ref(false);
const autosync = ref(false);
const autopush = ref(false);

onMounted(() => {
  if (store.config) {
    autopilot.value = store.config.autopilot;
    autotest.value = store.config.autotest;
    autosubmit.value = store.config.autosubmit;
    autosync.value = store.config.autosync;
    autopush.value = store.config.autopush;
  }
  document.addEventListener('keydown', onKey);
});
onUnmounted(() => document.removeEventListener('keydown', onKey));

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

const tabs: { key: TabKey; label: string }[] = [
  { key: 'appearance', label: 'Appearance' },
  { key: 'execution', label: 'Execution' },
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
