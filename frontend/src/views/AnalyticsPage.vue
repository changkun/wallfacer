<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api } from '../api/client';

const loading = ref(true);
const usage = ref<{ total_cost_usd: number; total_input_tokens: number; total_output_tokens: number } | null>(null);
const stats = ref<{ total: number; by_status: Record<string, number>; workspace_costs?: { workspace: string; cost_usd: number }[] } | null>(null);

onMounted(async () => {
  try {
    const [u, s] = await Promise.all([
      api<typeof usage.value>('GET', '/api/usage'),
      api<typeof stats.value>('GET', '/api/stats'),
    ]);
    usage.value = u;
    stats.value = s;
  } catch (e) { console.error('analytics:', e); }
  loading.value = false;
});

function fmtNum(n: number): string { return n?.toLocaleString() ?? '0'; }
function fmtCost(n: number): string { return n ? '$' + n.toFixed(2) : '$0.00'; }

const statusColors: Record<string, string> = {
  backlog: 'var(--col-backlog)', in_progress: 'var(--col-progress)',
  waiting: 'var(--col-waiting)', done: 'var(--col-done)',
  failed: 'var(--err)', cancelled: 'var(--ink-4)',
};
</script>

<template>
  <div class="analytics-page">
    <header class="page-header"><h1>Analytics</h1></header>

    <div v-if="loading" class="loading">Loading...</div>

    <div v-else class="analytics-body">
      <div class="card-grid">
        <div class="stat-card">
          <span class="stat-label">Total Cost</span>
          <span class="stat-value accent">{{ fmtCost(usage?.total_cost_usd ?? 0) }}</span>
        </div>
        <div class="stat-card">
          <span class="stat-label">Total Tasks</span>
          <span class="stat-value">{{ fmtNum(stats?.total ?? 0) }}</span>
        </div>
        <div class="stat-card">
          <span class="stat-label">Input Tokens</span>
          <span class="stat-value">{{ fmtNum(usage?.total_input_tokens ?? 0) }}</span>
        </div>
        <div class="stat-card">
          <span class="stat-label">Output Tokens</span>
          <span class="stat-value">{{ fmtNum(usage?.total_output_tokens ?? 0) }}</span>
        </div>
      </div>

      <section v-if="stats?.by_status" class="section">
        <h2>By Status</h2>
        <div class="status-list">
          <div v-for="(count, status) in stats.by_status" :key="status" class="status-row">
            <span class="status-dot" :style="{ background: statusColors[status as string] || 'var(--ink-4)' }" />
            <span class="status-name">{{ status }}</span>
            <span class="status-count">{{ count }}</span>
            <div class="status-bar-track">
              <div class="status-bar-fill" :style="{ width: (stats.total ? (count as number) / stats.total * 100 : 0) + '%', background: statusColors[status as string] || 'var(--ink-4)' }" />
            </div>
          </div>
        </div>
      </section>

      <section v-if="stats?.workspace_costs?.length" class="section">
        <h2>Workspace Costs</h2>
        <div class="ws-list">
          <div v-for="ws in stats.workspace_costs" :key="ws.workspace" class="ws-row">
            <span class="ws-name">{{ ws.workspace.split('/').pop() }}</span>
            <span class="ws-cost">{{ fmtCost(ws.cost_usd) }}</span>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.analytics-page { display: flex; flex-direction: column; height: 100%; overflow-y: auto; }
.page-header { padding: 12px 20px; border-bottom: 1px solid var(--rule); flex-shrink: 0; }
.page-header h1 { margin: 0; font-size: 15px; font-weight: 600; }
.loading { padding: 40px; text-align: center; color: var(--ink-4); }
.analytics-body { padding: 20px; display: flex; flex-direction: column; gap: 24px; }

.card-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; }
.stat-card {
  background: var(--bg-card); border: 1px solid var(--rule); border-radius: var(--r-sm);
  padding: 14px 16px; display: flex; flex-direction: column; gap: 4px;
}
.stat-label { font-size: 11px; color: var(--ink-3); text-transform: uppercase; letter-spacing: 0.04em; }
.stat-value { font-size: 20px; font-weight: 600; font-family: var(--font-mono); }
.stat-value.accent { color: var(--accent); }

.section h2 { font-size: 12px; text-transform: uppercase; color: var(--ink-3); letter-spacing: 0.04em; margin: 0 0 10px; }
.status-list { display: flex; flex-direction: column; gap: 6px; }
.status-row { display: flex; align-items: center; gap: 8px; font-size: 13px; }
.status-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.status-name { width: 100px; color: var(--ink-2); }
.status-count { width: 40px; text-align: right; font-family: var(--font-mono); font-size: 12px; color: var(--ink); }
.status-bar-track { flex: 1; height: 6px; background: var(--bg-sunk); border-radius: 3px; overflow: hidden; }
.status-bar-fill { height: 100%; border-radius: 3px; transition: width 0.3s; }

.ws-list { display: flex; flex-direction: column; gap: 4px; }
.ws-row { display: flex; justify-content: space-between; font-size: 13px; padding: 6px 0; border-bottom: 1px solid var(--rule); }
.ws-name { font-family: var(--font-mono); font-size: 12px; color: var(--ink-2); }
.ws-cost { font-family: var(--font-mono); font-size: 12px; color: var(--ink); }
</style>
