<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api } from '../api/client';

const loading = ref(true);
const usage = ref<{ total_cost_usd: number; total_input_tokens: number; total_output_tokens: number } | null>(null);
interface StatusBucket { count: number; cost_usd: number; input_tokens: number; output_tokens: number }
const stats = ref<{ total: number; by_status: Record<string, number | StatusBucket>; workspace_costs?: { workspace: string; cost_usd: number }[] } | null>(null);
function statusCount(v: number | StatusBucket): number {
  return typeof v === 'number' ? v : (v?.count ?? 0);
}

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
  <div class="analytics-mode" style="display: flex">
    <div class="analytics-mode__header">
      <div class="analytics-mode__heading">
        <span class="analytics-mode__eyebrow">Workspace</span>
        <h1 class="analytics-mode__title">Analytics</h1>
      </div>
    </div>

    <div class="analytics-mode__panels">
      <div v-if="loading" class="analytics-loading">Loading...</div>

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
            <div v-for="(bucket, status) in stats.by_status" :key="status" class="status-row">
              <span class="status-dot" :style="{ background: statusColors[status as string] || 'var(--ink-4)' }" />
              <span class="status-name">{{ status }}</span>
              <span class="status-count">{{ statusCount(bucket) }}</span>
              <div class="status-bar-track">
                <div class="status-bar-fill" :style="{ width: (stats.total ? statusCount(bucket) / stats.total * 100 : 0) + '%', background: statusColors[status as string] || 'var(--ink-4)' }" />
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
  </div>
</template>

<style scoped>
.analytics-loading { padding: 40px; text-align: center; color: var(--text-muted); }
.analytics-body { display: flex; flex-direction: column; gap: 24px; }

.card-grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 12px; }
.stat-card {
  background: var(--bg-card); border: 1px solid var(--border); border-radius: 6px;
  padding: 14px 16px; display: flex; flex-direction: column; gap: 4px;
}
.stat-label { font-size: 11px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.04em; }
.stat-value { font-size: 20px; font-weight: 600; font-family: var(--font-mono, ui-monospace, monospace); }
.stat-value.accent { color: var(--accent); }

.section h2 { font-size: 12px; text-transform: uppercase; color: var(--text-muted); letter-spacing: 0.04em; margin: 0 0 10px; }
.status-list { display: flex; flex-direction: column; gap: 6px; }
.status-row { display: flex; align-items: center; gap: 8px; font-size: 13px; }
.status-dot { width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0; }
.status-name { width: 100px; color: var(--text); }
.status-count { width: 40px; text-align: right; font-family: var(--font-mono, ui-monospace, monospace); font-size: 12px; color: var(--text); }
.status-bar-track { flex: 1; height: 6px; background: var(--bg-sunk, var(--bg-raised)); border-radius: 3px; overflow: hidden; }
.status-bar-fill { height: 100%; border-radius: 3px; transition: width 0.3s; }

.ws-list { display: flex; flex-direction: column; gap: 4px; }
.ws-row { display: flex; justify-content: space-between; font-size: 13px; padding: 6px 0; border-bottom: 1px solid var(--border); }
.ws-name { font-family: var(--font-mono, ui-monospace, monospace); font-size: 12px; color: var(--text-muted); }
.ws-cost { font-family: var(--font-mono, ui-monospace, monospace); font-size: 12px; color: var(--text); }
</style>
