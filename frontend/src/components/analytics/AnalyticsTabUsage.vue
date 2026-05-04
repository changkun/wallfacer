<script setup lang="ts">
import { ref, onMounted, watch } from 'vue';
import { api } from '../../api/client';

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

const STATUS_COLORS: Record<string, { bg: string; fg: string }> = {
  done: { bg: 'var(--badge-done-bg)', fg: 'var(--badge-done-fg)' },
  failed: { bg: 'var(--badge-failed-bg)', fg: 'var(--badge-failed-fg)' },
  cancelled: { bg: 'var(--badge-cancelled-bg)', fg: 'var(--badge-cancelled-fg)' },
  in_progress: { bg: 'var(--badge-inprogress-bg)', fg: 'var(--badge-inprogress-fg)' },
  waiting: { bg: 'var(--badge-waiting-bg)', fg: 'var(--badge-waiting-fg)' },
  backlog: { bg: 'var(--badge-backlog-bg)', fg: 'var(--badge-backlog-fg)' },
  committing: { bg: 'var(--badge-committing-bg)', fg: 'var(--badge-committing-fg)' },
};

const AGENT_LABELS: Record<string, string> = {
  implementation: 'Implementation',
  test: 'Test',
  refinement: 'Refinement',
  title: 'Title gen.',
  oversight: 'Oversight',
  'oversight-test': 'Oversight (test)',
  planning: 'Planning',
};

function statusColor(s: string) {
  return STATUS_COLORS[s] || { bg: 'var(--bg-raised)', fg: 'var(--text-muted)' };
}

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
    const cfg = await api<{ planning_window_days?: number }>('GET', '/api/config');
    if (cfg) {
      const n = parseInt(String(cfg.planning_window_days), 10);
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
  <div>
    <div
      style="
        display: flex;
        align-items: center;
        justify-content: space-between;
        margin-bottom: 16px;
      "
    >
      <h3 style="font-size: 16px; font-weight: 600; margin: 0">Analytics</h3>
      <div style="display: flex; align-items: center; gap: 12px">
        <label style="font-size: 12px; color: var(--text-muted)">
          Period:
          <select
            v-model="period"
            class="select"
            style="margin-left: 6px; font-size: 12px; padding: 2px 6px"
          >
            <option value="7">Last 7 days</option>
            <option value="30">Last 30 days</option>
            <option value="90">Last 90 days</option>
            <option value="0">All time</option>
          </select>
        </label>
      </div>
    </div>

    <div style="flex: 1; min-height: 0; overflow-y: auto">
      <div
        v-if="state === 'loading'"
        style="
          display: flex;
          align-items: center;
          justify-content: center;
          padding: 32px;
          color: var(--text-muted);
          font-size: 13px;
        "
      >
        Loading…
      </div>
      <div
        v-else-if="state === 'error'"
        style="
          padding: 12px;
          background: #f5d5d5;
          border-radius: 6px;
          font-size: 12px;
          color: #8c2020;
          font-family: monospace;
          white-space: pre-wrap;
        "
      >{{ errorMsg }}</div>
      <div
        v-else-if="state === 'empty'"
        style="
          text-align: center;
          padding: 32px;
          color: var(--text-muted);
          font-size: 13px;
        "
      >No usage data for the selected period.</div>
      <div v-else-if="state === 'content' && data">
        <div
          style="
            padding: 4px 0 16px;
            font-size: 12px;
            color: var(--text-muted);
          "
        >
          {{ (data.task_count ?? 0) }} task{{ (data.task_count ?? 0) === 1 ? '' : 's' }}
          ·
          {{ data.period_days === 0 ? 'all time' : 'last ' + data.period_days + ' days' }}
          ·
          total cost: {{ data.total?.cost_usd ? '$' + data.total.cost_usd.toFixed(4) : '$0.0000' }}
        </div>

        <div style="margin-bottom: 20px">
          <div
            style="
              font-size: 11px;
              font-weight: 600;
              color: var(--text-muted);
              text-transform: uppercase;
              letter-spacing: 0.5px;
              margin-bottom: 8px;
            "
          >By Status</div>
          <div style="overflow-x: auto">
            <table style="width: 100%; border-collapse: collapse; font-size: 12px">
              <thead>
                <tr style="border-bottom: 1px solid var(--border)">
                  <th style="text-align: left; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px;">Status</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Input</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Output</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Total Tokens</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Cost</th>
                </tr>
              </thead>
              <tbody>
                <template v-if="Object.keys(data.by_status || {}).length === 0">
                  <tr><td colspan="5" style="padding: 12px; text-align: center; color: var(--text-muted); font-size: 12px;">No data</td></tr>
                </template>
                <template v-else>
                  <tr v-for="status in Object.keys(data.by_status || {}).sort()" :key="status">
                    <td style="padding: 6px 10px;">
                      <span
                        :style="{
                          display: 'inline-block',
                          padding: '1px 7px',
                          borderRadius: '999px',
                          fontSize: '11px',
                          fontWeight: 600,
                          background: statusColor(status).bg,
                          color: statusColor(status).fg,
                        }"
                      >{{ status.replace('_', ' ') }}</span>
                    </td>
                    <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmtTokens((data.by_status || {})[status].input_tokens) }}</td>
                    <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmtTokens((data.by_status || {})[status].output_tokens) }}</td>
                    <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmtTokens(totalTokens((data.by_status || {})[status])) }}</td>
                    <td style="padding: 6px 10px; text-align: right; font-weight: 600; color: var(--accent);">{{ fmtCost((data.by_status || {})[status].cost_usd) }}</td>
                  </tr>
                  <tr>
                    <td style="padding: 6px 10px; font-weight: 600;"><strong>Total</strong></td>
                    <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmtTokens(data.total?.input_tokens) }}</td>
                    <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmtTokens(data.total?.output_tokens) }}</td>
                    <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmtTokens(totalTokens(data.total || {})) }}</td>
                    <td style="padding: 6px 10px; text-align: right; font-weight: 600; color: var(--accent);">{{ fmtCost(data.total?.cost_usd) }}</td>
                  </tr>
                </template>
              </tbody>
            </table>
          </div>
        </div>

        <div>
          <div
            style="
              font-size: 11px;
              font-weight: 600;
              color: var(--text-muted);
              text-transform: uppercase;
              letter-spacing: 0.5px;
              margin-bottom: 8px;
            "
          >By Sub-Agent</div>
          <div style="overflow-x: auto">
            <table style="width: 100%; border-collapse: collapse; font-size: 12px">
              <thead>
                <tr style="border-bottom: 1px solid var(--border)">
                  <th style="text-align: left; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px;">Agent</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Input</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Output</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Total Tokens</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Cost</th>
                </tr>
              </thead>
              <tbody>
                <template v-if="Object.keys(data.by_sub_agent || {}).length === 0">
                  <tr><td colspan="5" style="padding: 12px; text-align: center; color: var(--text-muted); font-size: 12px;">No data</td></tr>
                </template>
                <template v-else>
                  <tr v-for="key in Object.keys(data.by_sub_agent || {}).sort()" :key="key">
                    <td style="padding: 6px 10px;">{{ agentLabel(key) }}</td>
                    <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmtTokens((data.by_sub_agent || {})[key].input_tokens) }}</td>
                    <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmtTokens((data.by_sub_agent || {})[key].output_tokens) }}</td>
                    <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmtTokens(totalTokens((data.by_sub_agent || {})[key])) }}</td>
                    <td style="padding: 6px 10px; text-align: right; font-weight: 600; color: var(--accent);">{{ fmtCost((data.by_sub_agent || {})[key].cost_usd) }}</td>
                  </tr>
                  <tr>
                    <td style="padding: 6px 10px; font-weight: 600;"><strong>Total</strong></td>
                    <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmtTokens(data.total?.input_tokens) }}</td>
                    <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmtTokens(data.total?.output_tokens) }}</td>
                    <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmtTokens(totalTokens(data.total || {})) }}</td>
                    <td style="padding: 6px 10px; text-align: right; font-weight: 600; color: var(--accent);">{{ fmtCost(data.total?.cost_usd) }}</td>
                  </tr>
                </template>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
