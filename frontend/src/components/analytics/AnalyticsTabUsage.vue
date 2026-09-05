<script setup lang="ts">
import { ref, onMounted, watch } from 'vue';
import { api } from '../../api/client';
import AppSelect from '../AppSelect.vue';
import { statusPill } from '../../lib/statusPill';

const PERIOD_OPTIONS = [
  { value: '7', label: 'Last 7 days' },
  { value: '30', label: 'Last 30 days' },
  { value: '90', label: 'Last 90 days' },
  { value: '0', label: 'All time' },
];

interface UsageBucket {
  input_tokens?: number;
  output_tokens?: number;
  cost_usd?: number;
}

interface UsageResponse {
  total?: UsageBucket;
  by_status?: Record<string, UsageBucket>;
  by_sub_agent?: Record<string, UsageBucket>;
  task_count?: number;
  period_days?: number;
}

const STATE = {
  LOADING: 'loading',
  ERROR: 'error',
  EMPTY: 'empty',
  CONTENT: 'content',
} as const;
type State = typeof STATE[keyof typeof STATE];

const state = ref<State>(STATE.LOADING);
const errorMsg = ref('');
const data = ref<UsageResponse | null>(null);
const period = ref('7');

const AGENT_LABELS: Record<string, string> = {
  implementation: 'Implementation',
  test: 'Test',
  refinement: 'Refinement',
  title: 'Title gen.',
  oversight: 'Oversight',
  'oversight-test': 'Oversight (test)',
  'agent-session': 'Agent Session',
};

function agentLabel(k: string) {
  return AGENT_LABELS[k] || k;
}

function fmtTokens(n?: number) {
  if (!n) return '—';
  return n.toLocaleString();
}

function fmtCost(usd?: number) {
  if (!usd) return '—';
  return '$' + usd.toFixed(4);
}

function totalTokens(b: UsageBucket) {
  return (b.input_tokens || 0) + (b.output_tokens || 0);
}

async function fetchStats() {
  state.value = STATE.LOADING;
  try {
    const days = period.value;
    const r = await api<UsageResponse>('GET', `/api/usage?days=${encodeURIComponent(days)}`);
    data.value = r;
    const total = r.total || {};
    const byStatus = r.by_status || {};
    const bySubAgent = r.by_sub_agent || {};
    const hasData = (total.cost_usd ?? 0) > 0
      || Object.keys(byStatus).length > 0
      || Object.keys(bySubAgent).length > 0;
    if (!hasData && r.task_count === 0) {
      state.value = STATE.EMPTY;
      return;
    }
    state.value = STATE.CONTENT;
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
    state.value = STATE.ERROR;
  }
}

async function seedPeriodFromConfig() {
  try {
    const cfg = await api<{ agent_session_window_days?: number }>('GET', '/api/config');
    if (cfg) {
      const n = parseInt(String(cfg.agent_session_window_days), 10);
      if (!Number.isNaN(n) && n >= 0) period.value = String(n);
    }
  } catch { /* ignore */ }
}

onMounted(async () => {
  await seedPeriodFromConfig();
  fetchStats();
});

watch(period, () => fetchStats());
</script>

<template>
  <div class="an-body">
    <div class="an-head an-head--tab">
      <p v-if="state === 'content' && data" class="an-note">
        <strong>{{ (data.task_count ?? 0) }}</strong> task{{ (data.task_count ?? 0) === 1 ? '' : 's' }},
        {{ data.period_days === 0 ? 'all time' : 'last ' + data.period_days + ' days' }},
        total cost <strong>{{ data.total?.cost_usd ? '$' + data.total.cost_usd.toFixed(4) : '$0.0000' }}</strong>
      </p>
      <span v-else></span>
      <label class="an-tools">
        Period
        <AppSelect v-model="period" :options="PERIOD_OPTIONS" aria-label="Period" />
      </label>
    </div>

    <div v-if="state === 'loading'" class="an-state">Loading…</div>
    <div v-else-if="state === 'error'" class="an-error">{{ errorMsg }}</div>
    <div v-else-if="state === 'empty'" class="an-state">No usage data for the selected period.</div>
    <template v-else-if="state === 'content' && data">
      <section class="card an-section">
        <div class="card-head"><span class="eyebrow">By status</span></div>
        <div class="an-table-wrap">
          <table class="an-table">
            <thead>
              <tr><th>Status</th><th class="num">Input</th><th class="num">Output</th><th class="num">Total tokens</th><th class="num">Cost</th></tr>
            </thead>
            <tbody>
              <tr v-if="Object.keys(data.by_status || {}).length === 0"><td colspan="5" class="empty">No data</td></tr>
              <template v-else>
                <tr v-for="status in Object.keys(data.by_status || {}).sort()" :key="status">
                  <td><span class="pill" :class="statusPill(status)">{{ status.replace('_', ' ') }}</span></td>
                  <td class="num">{{ fmtTokens((data.by_status || {})[status].input_tokens) }}</td>
                  <td class="num">{{ fmtTokens((data.by_status || {})[status].output_tokens) }}</td>
                  <td class="num">{{ fmtTokens(totalTokens((data.by_status || {})[status])) }}</td>
                  <td class="num accent">{{ fmtCost((data.by_status || {})[status].cost_usd) }}</td>
                </tr>
                <tr class="total">
                  <td>Total</td>
                  <td class="num">{{ fmtTokens(data.total?.input_tokens) }}</td>
                  <td class="num">{{ fmtTokens(data.total?.output_tokens) }}</td>
                  <td class="num">{{ fmtTokens(totalTokens(data.total || {})) }}</td>
                  <td class="num accent">{{ fmtCost(data.total?.cost_usd) }}</td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
      </section>

      <section class="card an-section">
        <div class="card-head"><span class="eyebrow">By sub-agent</span></div>
        <div class="an-table-wrap">
          <table class="an-table">
            <thead>
              <tr><th>Agent</th><th class="num">Input</th><th class="num">Output</th><th class="num">Total tokens</th><th class="num">Cost</th></tr>
            </thead>
            <tbody>
              <tr v-if="Object.keys(data.by_sub_agent || {}).length === 0"><td colspan="5" class="empty">No data</td></tr>
              <template v-else>
                <tr v-for="key in Object.keys(data.by_sub_agent || {}).sort()" :key="key">
                  <td class="strong">{{ agentLabel(key) }}</td>
                  <td class="num">{{ fmtTokens((data.by_sub_agent || {})[key].input_tokens) }}</td>
                  <td class="num">{{ fmtTokens((data.by_sub_agent || {})[key].output_tokens) }}</td>
                  <td class="num">{{ fmtTokens(totalTokens((data.by_sub_agent || {})[key])) }}</td>
                  <td class="num accent">{{ fmtCost((data.by_sub_agent || {})[key].cost_usd) }}</td>
                </tr>
                <tr class="total">
                  <td>Total</td>
                  <td class="num">{{ fmtTokens(data.total?.input_tokens) }}</td>
                  <td class="num">{{ fmtTokens(data.total?.output_tokens) }}</td>
                  <td class="num">{{ fmtTokens(totalTokens(data.total || {})) }}</td>
                  <td class="num accent">{{ fmtCost(data.total?.cost_usd) }}</td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
      </section>
    </template>
  </div>
</template>
