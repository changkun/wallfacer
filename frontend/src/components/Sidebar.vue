<script setup lang="ts">
import { useRoute } from 'vue-router';
import { useTaskStore } from '../stores/tasks';
import { useTheme } from '../composables/useTheme';

const route = useRoute();

const store = useTaskStore();
const { theme, cycle } = useTheme();

defineProps<{ collapsed: boolean }>();
const emit = defineEmits<{ toggle: []; settings: [] }>();

function themeLabel(): string {
  switch (theme.value) {
    case 'light': return 'Light';
    case 'dark': return 'Dark';
    default: return 'Auto';
  }
}
</script>

<template>
  <aside class="sidebar" :class="{ collapsed }">
    <div class="sb-top">
      <div class="sb-brand" @click="emit('toggle')">
        <span class="sb-logo">W</span>
        <span v-if="!collapsed" class="sb-name">Wallfacer</span>
      </div>
    </div>

    <nav v-if="!collapsed" class="sb-nav">
      <router-link to="/" class="sb-item" :class="{ active: route.path === '/' }">
        <span class="sb-icon">☰</span>
        <span>Board</span>
      </router-link>
      <router-link to="/agents" class="sb-item" :class="{ active: route.path === '/agents' }">
        <span class="sb-icon">◆</span>
        <span>Agents</span>
      </router-link>
      <router-link to="/flows" class="sb-item" :class="{ active: route.path === '/flows' }">
        <span class="sb-icon">→</span>
        <span>Flows</span>
      </router-link>
      <router-link to="/terminal" class="sb-item" :class="{ active: route.path === '/terminal' }">
        <span class="sb-icon">▸</span>
        <span>Terminal</span>
      </router-link>
      <router-link to="/analytics" class="sb-item" :class="{ active: route.path === '/analytics' }">
        <span class="sb-icon">▪</span>
        <span>Analytics</span>
      </router-link>
    </nav>

    <div v-if="!collapsed" class="sb-stats">
      <div class="sb-stat">
        <span class="sb-stat-label">Tasks</span>
        <span class="sb-stat-value">{{ store.tasks.length }}</span>
      </div>
      <div class="sb-stat">
        <span class="sb-stat-label">Running</span>
        <span class="sb-stat-value">{{ store.inProgress.length }}</span>
      </div>
      <div class="sb-stat">
        <span class="sb-stat-label">Cost</span>
        <span class="sb-stat-value">{{ store.tasks.reduce((s, t) => s + (t.usage?.cost_usd || 0), 0).toFixed(2) }}</span>
      </div>
    </div>

    <div class="sb-bottom">
      <button v-if="!collapsed" class="sb-btn" @click="emit('settings')">
        <span class="sb-icon">⚙</span>
        <span>Settings</span>
      </button>
      <button v-if="!collapsed" class="sb-btn" @click="cycle" :title="'Theme: ' + theme">
        {{ theme === 'light' ? '☀' : theme === 'dark' ? '☾' : '◐' }}
        <span>{{ themeLabel() }}</span>
      </button>
      <button class="sb-btn sb-collapse-btn" @click="emit('toggle')" :title="collapsed ? 'Expand' : 'Collapse'">
        {{ collapsed ? '→' : '←' }}
      </button>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  width: 200px;
  background: var(--bg-sunk, #ebe7de);
  border-right: 1px solid var(--rule);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  transition: width 0.15s;
  overflow: hidden;
}
.sidebar.collapsed {
  width: 44px;
}

.sb-top {
  padding: 8px;
  border-bottom: 1px solid var(--rule);
}
.sb-brand {
  display: flex;
  align-items: center;
  gap: 8px;
  cursor: pointer;
  padding: 4px;
  border-radius: var(--r-sm);
}
.sb-brand:hover { background: var(--bg-hover); }
.sb-logo {
  width: 24px;
  height: 24px;
  background: var(--accent);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: var(--r-sm);
  font-weight: 700;
  font-size: 13px;
  flex-shrink: 0;
}
.sb-name {
  font-weight: 600;
  font-size: 13px;
  white-space: nowrap;
}

.sb-nav {
  padding: 8px;
  flex: 1;
}
.sb-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: var(--r-sm);
  font-size: 12px;
  color: var(--ink-2);
  cursor: pointer;
  text-decoration: none;
}
.sb-item:hover { background: var(--bg-hover); }
.sb-item.active {
  background: var(--bg-active);
  color: var(--ink);
  font-weight: 500;
}
.sb-icon { font-size: 14px; }

.sb-stats {
  padding: 8px 16px;
  border-top: 1px solid var(--rule);
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.sb-stat {
  display: flex;
  justify-content: space-between;
  font-size: 11px;
}
.sb-stat-label { color: var(--ink-3); }
.sb-stat-value { font-family: var(--font-mono); color: var(--ink-2); }

.sb-bottom {
  padding: 8px;
  border-top: 1px solid var(--rule);
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.sb-btn {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 8px;
  background: none;
  border: none;
  border-radius: var(--r-sm);
  color: var(--ink-3);
  font-size: 12px;
  cursor: pointer;
  white-space: nowrap;
}
.sb-btn:hover { background: var(--bg-hover); color: var(--ink); }
.sb-collapse-btn { justify-content: center; }
</style>
