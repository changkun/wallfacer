<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue';
import { api } from '../api/client';
import { useTaskStore } from '../stores/tasks';
import { useTheme } from '../composables/useTheme';

const emit = defineEmits<{ close: [] }>();
const store = useTaskStore();
const { theme, cycle } = useTheme();

const activeTab = ref('execution');
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

function onBackdrop(e: MouseEvent) {
  if ((e.target as HTMLElement).classList.contains('modal-backdrop')) emit('close');
}

const tabs = [
  { key: 'execution', label: 'Execution' },
  { key: 'appearance', label: 'Appearance' },
  { key: 'about', label: 'About' },
];
</script>

<template>
  <div class="modal-backdrop" @click="onBackdrop">
    <div class="modal">
      <header class="modal-header">
        <h2>Settings</h2>
        <button class="modal-close" @click="emit('close')">&times;</button>
      </header>

      <div class="modal-body">
        <nav class="modal-tabs">
          <button
            v-for="tab in tabs" :key="tab.key"
            :class="{ active: activeTab === tab.key }"
            @click="activeTab = tab.key"
          >{{ tab.label }}</button>
        </nav>

        <div class="modal-content">
          <div v-if="activeTab === 'execution'" class="tab-content">
            <div class="setting-row">
              <label><input type="checkbox" v-model="autopilot" /> Autopilot</label>
              <span class="setting-desc">Auto-promote backlog tasks to in-progress</span>
            </div>
            <div class="setting-row">
              <label><input type="checkbox" v-model="autotest" /> Auto-test</label>
              <span class="setting-desc">Run test agent after implementation</span>
            </div>
            <div class="setting-row">
              <label><input type="checkbox" v-model="autosubmit" /> Auto-submit</label>
              <span class="setting-desc">Auto-commit when agent finishes</span>
            </div>
            <div class="setting-row">
              <label><input type="checkbox" v-model="autosync" /> Auto-sync</label>
              <span class="setting-desc">Rebase worktrees on main before execution</span>
            </div>
            <div class="setting-row">
              <label><input type="checkbox" v-model="autopush" /> Auto-push</label>
              <span class="setting-desc">Push after commit pipeline completes</span>
            </div>
            <button class="save-btn" @click="saveExecution" :disabled="saving">
              {{ saving ? 'Saving...' : 'Save' }}
            </button>
          </div>

          <div v-else-if="activeTab === 'appearance'" class="tab-content">
            <div class="setting-row">
              <span>Theme</span>
              <button class="theme-btn" @click="cycle">
                {{ theme === 'light' ? '☀ Light' : theme === 'dark' ? '☾ Dark' : '◐ Auto' }}
              </button>
            </div>
          </div>

          <div v-else-if="activeTab === 'about'" class="tab-content">
            <div class="about-info">
              <p><strong>Wallfacer</strong></p>
              <p>Autonomous engineering platform.</p>
              <p class="about-meta">Task board runner for AI agents.</p>
            </div>
          </div>
        </div>
      </div>
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
  box-shadow: var(--sh-pop, 0 12px 40px rgba(0,0,0,0.18));
  display: flex;
  flex-direction: column;
  overflow: hidden;
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
}
.modal-close {
  background: none;
  border: none;
  font-size: 20px;
  color: var(--ink-3);
  cursor: pointer;
}
.modal-close:hover { color: var(--ink); }

.modal-body {
  display: flex;
  flex: 1;
  overflow: hidden;
}
.modal-tabs {
  display: flex;
  flex-direction: column;
  padding: 12px 0;
  border-right: 1px solid var(--rule);
  min-width: 120px;
}
.modal-tabs button {
  padding: 6px 16px;
  background: none;
  border: none;
  text-align: left;
  font-size: 12px;
  color: var(--ink-2);
  cursor: pointer;
}
.modal-tabs button:hover { background: var(--bg-hover); }
.modal-tabs button.active {
  color: var(--ink);
  font-weight: 500;
  background: var(--bg-active);
}

.modal-content {
  flex: 1;
  padding: 16px 20px;
  overflow-y: auto;
}

.tab-content {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.setting-row {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.setting-row label {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  cursor: pointer;
}
.setting-desc {
  font-size: 11px;
  color: var(--ink-3);
  padding-left: 24px;
}

.save-btn {
  align-self: flex-start;
  margin-top: 8px;
  padding: 5px 16px;
  background: var(--accent);
  color: #fff;
  border: none;
  border-radius: var(--r-sm);
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}
.save-btn:hover { background: var(--accent-2); }
.save-btn:disabled { opacity: 0.4; }

.theme-btn {
  padding: 4px 12px;
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  background: var(--bg-card);
  color: var(--ink);
  font-size: 12px;
  cursor: pointer;
  align-self: flex-start;
}
.theme-btn:hover { background: var(--bg-hover); }

.about-info p { margin: 4px 0; font-size: 13px; }
.about-meta { color: var(--ink-3); font-size: 12px; }
</style>
