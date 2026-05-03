<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api } from '../api/client';

interface Agent {
  slug: string;
  name: string;
  description: string;
  builtin: boolean;
  sandbox: string;
  model: string;
}

const agents = ref<Agent[]>([]);
const loading = ref(true);
const selectedAgent = ref<Agent | null>(null);
const agentDetail = ref<{ prompt_template?: string } | null>(null);

onMounted(async () => {
  try {
    agents.value = await api<Agent[]>('GET', '/api/agents');
  } catch (e) { console.error('agents:', e); }
  loading.value = false;
});

async function selectAgent(a: Agent) {
  selectedAgent.value = a;
  try {
    agentDetail.value = await api('GET', `/api/agents/${a.slug}`);
  } catch (e) { console.error('agent detail:', e); }
}
</script>

<template>
  <div class="agents-page">
    <header class="page-header">
      <h1>Agents</h1>
    </header>

    <div class="agents-layout">
      <div class="agents-list">
        <div v-if="loading" class="agents-empty">Loading...</div>
        <div
          v-for="a in agents" :key="a.slug"
          class="agent-row"
          :class="{ selected: selectedAgent?.slug === a.slug }"
          @click="selectAgent(a)"
        >
          <div class="agent-name">
            {{ a.name || a.slug }}
            <span v-if="a.builtin" class="agent-badge">built-in</span>
          </div>
          <div class="agent-desc">{{ a.description }}</div>
          <div class="agent-meta">
            <span v-if="a.sandbox">{{ a.sandbox }}</span>
            <span v-if="a.model">{{ a.model }}</span>
          </div>
        </div>
      </div>

      <div v-if="selectedAgent && agentDetail" class="agent-detail">
        <h2>{{ selectedAgent.name || selectedAgent.slug }}</h2>
        <p class="detail-desc">{{ selectedAgent.description }}</p>
        <div class="detail-meta">
          <span>Sandbox: {{ selectedAgent.sandbox || 'default' }}</span>
          <span>Model: {{ selectedAgent.model || 'default' }}</span>
        </div>
        <div v-if="agentDetail.prompt_template" class="detail-prompt">
          <h3>Prompt Template</h3>
          <pre>{{ agentDetail.prompt_template }}</pre>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.agents-page {
  display: flex;
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}
.page-header {
  padding: 12px 20px;
  border-bottom: 1px solid var(--rule);
}
.page-header h1 { margin: 0; font-size: 15px; font-weight: 600; }

.agents-layout {
  display: flex;
  flex: 1;
  overflow: hidden;
}
.agents-list {
  width: 360px;
  border-right: 1px solid var(--rule);
  overflow-y: auto;
}
.agents-empty { padding: 20px; text-align: center; color: var(--ink-4); }
.agent-row {
  padding: 10px 16px;
  border-bottom: 1px solid var(--rule);
  cursor: pointer;
}
.agent-row:hover { background: var(--bg-hover); }
.agent-row.selected { background: var(--bg-active); }
.agent-name { font-size: 13px; font-weight: 500; display: flex; align-items: center; gap: 6px; }
.agent-badge {
  font-size: 9px; padding: 1px 5px; border-radius: 2px;
  background: var(--accent-tint); color: var(--accent); font-weight: 500;
}
.agent-desc { font-size: 11px; color: var(--ink-3); margin-top: 2px; }
.agent-meta { font-size: 10px; color: var(--ink-4); font-family: var(--font-mono); margin-top: 4px; display: flex; gap: 8px; }

.agent-detail {
  flex: 1;
  padding: 16px 20px;
  overflow-y: auto;
}
.agent-detail h2 { margin: 0 0 4px; font-size: 16px; }
.detail-desc { color: var(--ink-2); font-size: 13px; margin: 0 0 12px; }
.detail-meta { font-size: 12px; color: var(--ink-3); font-family: var(--font-mono); display: flex; gap: 16px; margin-bottom: 16px; }
.detail-prompt h3 { font-size: 11px; text-transform: uppercase; color: var(--ink-3); margin: 0 0 6px; }
.detail-prompt pre {
  font-family: var(--font-mono); font-size: 11px; color: var(--ink-2);
  background: var(--bg-sunk); padding: 12px; border-radius: var(--r-sm);
  white-space: pre-wrap; word-break: break-word; line-height: 1.5; margin: 0;
}
</style>
