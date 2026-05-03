<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api } from '../api/client';

interface Flow {
  slug: string;
  name: string;
  description: string;
  builtin: boolean;
  steps: { agent: string; label: string }[];
}

const flows = ref<Flow[]>([]);
const loading = ref(true);
const selectedFlow = ref<Flow | null>(null);

onMounted(async () => {
  try {
    flows.value = await api<Flow[]>('GET', '/api/flows');
  } catch (e) { console.error('flows:', e); }
  loading.value = false;
});

async function selectFlow(f: Flow) {
  selectedFlow.value = f;
  try {
    const detail = await api<Flow>('GET', `/api/flows/${f.slug}`);
    selectedFlow.value = detail;
  } catch (e) { console.error('flow detail:', e); }
}
</script>

<template>
  <div class="flows-page">
    <header class="page-header">
      <h1>Flows</h1>
    </header>

    <div class="flows-layout">
      <div class="flows-list">
        <div v-if="loading" class="flows-empty">Loading...</div>
        <div
          v-for="f in flows" :key="f.slug"
          class="flow-row"
          :class="{ selected: selectedFlow?.slug === f.slug }"
          @click="selectFlow(f)"
        >
          <div class="flow-name">
            {{ f.name || f.slug }}
            <span v-if="f.builtin" class="flow-badge">built-in</span>
          </div>
          <div class="flow-desc">{{ f.description }}</div>
        </div>
      </div>

      <div v-if="selectedFlow" class="flow-detail">
        <h2>{{ selectedFlow.name || selectedFlow.slug }}</h2>
        <p class="detail-desc">{{ selectedFlow.description }}</p>
        <div v-if="selectedFlow.steps?.length" class="flow-steps">
          <h3>Steps</h3>
          <div v-for="(step, i) in selectedFlow.steps" :key="i" class="flow-step">
            <span class="step-num">{{ i + 1 }}</span>
            <span class="step-agent">{{ step.agent }}</span>
            <span v-if="step.label" class="step-label">{{ step.label }}</span>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.flows-page {
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

.flows-layout {
  display: flex;
  flex: 1;
  overflow: hidden;
}
.flows-list {
  width: 360px;
  border-right: 1px solid var(--rule);
  overflow-y: auto;
}
.flows-empty { padding: 20px; text-align: center; color: var(--ink-4); }
.flow-row {
  padding: 10px 16px;
  border-bottom: 1px solid var(--rule);
  cursor: pointer;
}
.flow-row:hover { background: var(--bg-hover); }
.flow-row.selected { background: var(--bg-active); }
.flow-name { font-size: 13px; font-weight: 500; display: flex; align-items: center; gap: 6px; }
.flow-badge {
  font-size: 9px; padding: 1px 5px; border-radius: 2px;
  background: var(--accent-tint); color: var(--accent); font-weight: 500;
}
.flow-desc { font-size: 11px; color: var(--ink-3); margin-top: 2px; }

.flow-detail {
  flex: 1;
  padding: 16px 20px;
  overflow-y: auto;
}
.flow-detail h2 { margin: 0 0 4px; font-size: 16px; }
.detail-desc { color: var(--ink-2); font-size: 13px; margin: 0 0 16px; }
.flow-steps h3 { font-size: 11px; text-transform: uppercase; color: var(--ink-3); margin: 0 0 8px; }
.flow-step {
  display: flex; align-items: center; gap: 8px;
  padding: 6px 0; border-bottom: 1px solid var(--rule);
  font-size: 12px;
}
.step-num {
  width: 20px; height: 20px; border-radius: 50%;
  background: var(--accent-tint); color: var(--accent);
  display: flex; align-items: center; justify-content: center;
  font-size: 10px; font-weight: 600; flex-shrink: 0;
}
.step-agent { font-family: var(--font-mono); font-weight: 500; }
.step-label { color: var(--ink-3); }
</style>
