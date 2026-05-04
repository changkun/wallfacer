<script setup lang="ts">
import { ref, onMounted, computed } from 'vue';
import { api } from '../../api/client';

interface PhaseStats {
  count: number;
  min_ms?: number;
  max_ms?: number;
  sum_ms?: number;
  p50_ms?: number;
  p95_ms?: number;
  p99_ms?: number;
}

interface DailyCount { date: string; count: number }

interface ThroughputData {
  total_completed?: number;
  total_failed?: number;
  success_rate_pct?: number;
  median_execution_s?: number;
  p95_execution_s?: number;
  daily_completions?: DailyCount[];
}

interface SpansResponse {
  phases?: Record<string, PhaseStats>;
  throughput?: ThroughputData;
  tasks_scanned?: number;
  spans_total?: number;
}

const STATE = { LOADING: 'loading', ERROR: 'error', EMPTY: 'empty', TABLE: 'table' } as const;
type State = typeof STATE[keyof typeof STATE];

const state = ref<State>(STATE.LOADING);
const errorMsg = ref('');
const data = ref<SpansResponse | null>(null);

const PHASE_INFO: Record<string, { label: string; desc: string }> = {
  worktree_setup: { label: 'Worktree Setup', desc: 'Creates an isolated git worktree for the task' },
  agent_turn: { label: 'Agent Turn', desc: 'One execution turn of the Claude Code agent (start → stop_reason)' },
  container_run: { label: 'Container Run', desc: 'Full sandbox container lifecycle from start to exit' },
  commit: { label: 'Commit Pipeline', desc: 'Commits and pushes task changes to the git repository' },
};

function phaseLabel(p: string) { return PHASE_INFO[p]?.label || p; }
function phaseDesc(p: string) { return PHASE_INFO[p]?.desc || 'Custom execution phase'; }

function fmtMs(ms?: number) {
  if (ms === undefined || ms === null) return '—';
  if (ms < 1000) return ms + 'ms';
  return (ms / 1000).toFixed(1) + 's';
}

function fmtSeconds(s?: number) {
  if (s === undefined || s === null || s === 0) return '—';
  if (s < 60) return s.toFixed(1) + 's';
  return (s / 60).toFixed(1) + 'm';
}

function colorStyleForMs(ms?: number) {
  if (ms == null) return '';
  if (ms < 5000) return 'color:#22863a;';
  if (ms < 30000) return 'color:#d97706;';
  return 'color:#dc2626;';
}

const sortedPhaseKeys = computed(() => Object.keys(data.value?.phases || {}).sort());

const globalMaxMs = computed(() => {
  const phases = data.value?.phases || {};
  let m = 0;
  for (const k of Object.keys(phases)) {
    const v = phases[k].max_ms || 0;
    if (v > m) m = v;
  }
  return m;
});

function barPct(p50?: number) {
  if (!globalMaxMs.value || p50 == null) return 0;
  return Math.min(100, Math.round((p50 / globalMaxMs.value) * 100));
}

const tiles = computed(() => {
  const tp = data.value?.throughput || {};
  const hasData = (tp.total_completed || 0) > 0 || (tp.total_failed || 0) > 0;
  return [
    { label: 'Completed', value: hasData ? String(tp.total_completed) : '—' },
    { label: 'Failed', value: hasData ? String(tp.total_failed) : '—' },
    { label: 'Success', value: hasData ? (tp.success_rate_pct || 0).toFixed(1) + '%' : '—' },
    { label: 'Median', value: fmtSeconds(tp.median_execution_s) },
    { label: 'P95', value: fmtSeconds(tp.p95_execution_s) },
  ];
});

const dailyMaxCount = computed(() => {
  const daily = data.value?.throughput?.daily_completions || [];
  let m = 0;
  for (const d of daily) { if (d.count > m) m = d.count; }
  return m;
});

function dailyBarHeight(count: number) {
  return dailyMaxCount.value > 0
    ? Math.max(4, Math.round((count / dailyMaxCount.value) * 100))
    : 4;
}

function meanMs(s: PhaseStats) {
  if (!s.count) return '—';
  return ((s.sum_ms || 0) / s.count).toFixed(0) + ' ms';
}

async function fetchStats() {
  state.value = STATE.LOADING;
  try {
    const r = await api<SpansResponse>('GET', '/api/debug/spans');
    data.value = r;
    const phases = r.phases || {};
    const tp = r.throughput || {};
    const hasThroughput = (tp.total_completed || 0) > 0 || (tp.total_failed || 0) > 0;
    if (Object.keys(phases).length === 0 && !hasThroughput) {
      state.value = STATE.EMPTY;
      return;
    }
    state.value = STATE.TABLE;
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
    state.value = STATE.ERROR;
  }
}

onMounted(() => fetchStats());
</script>

<template>
  <div>
    <div
      style="
        display: flex;
        align-items: flex-start;
        justify-content: space-between;
        margin-bottom: 16px;
      "
    >
      <div>
        <h3 style="font-size: 16px; font-weight: 600; margin: 0">Analytics</h3>
        <div style="font-size: 12px; color: var(--text-muted); margin-top: 3px">
          Per-phase latency aggregated across all tasks
        </div>
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
      >Loading…</div>
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
      >No span data yet. Run a task to collect timing data.</div>
      <div v-else-if="state === 'table' && data">
        <div
          style="
            display: flex;
            gap: 20px;
            flex-wrap: wrap;
            padding: 10px 0 14px;
            border-bottom: 1px solid var(--border);
            margin-bottom: 12px;
          "
        >
          <div
            v-for="tile in tiles"
            :key="tile.label"
            style="display: flex; flex-direction: column; gap: 2px; min-width: 72px;"
          >
            <span style="font-size: 11px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.4px;">{{ tile.label }}</span>
            <span style="font-size: 20px; font-weight: 700; line-height: 1.2;">{{ tile.value }}</span>
          </div>
          <div
            v-if="(data.throughput?.daily_completions || []).length"
            style="display: flex; flex-direction: column; gap: 4px; flex: 1; min-width: 160px;"
          >
            <span style="font-size: 11px; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.4px;">Daily completions (30d)</span>
            <div style="display: flex; gap: 2px; align-items: flex-end; height: 36px;">
              <div
                v-for="d in (data.throughput?.daily_completions || [])"
                :key="d.date"
                :title="d.date + ': ' + d.count"
                style="flex: 1; height: 32px; display: flex; align-items: flex-end;"
              >
                <div
                  :style="{
                    width: '100%',
                    height: dailyBarHeight(d.count) + '%',
                    background: d.count > 0 ? 'var(--accent)' : 'var(--border)',
                    borderRadius: '2px 2px 0 0',
                  }"
                />
              </div>
            </div>
          </div>
        </div>

        <div style="padding: 8px 0 12px; font-size: 12px; color: var(--text-muted);">
          <strong>{{ data.tasks_scanned }}</strong> tasks scanned ·
          <strong>{{ data.spans_total }}</strong> spans across
          <strong>{{ sortedPhaseKeys.length }}</strong> phase{{ sortedPhaseKeys.length === 1 ? '' : 's' }}
        </div>

        <div style="overflow-x: auto">
          <table style="width: 100%; border-collapse: collapse; font-size: 12px">
            <thead>
              <tr style="border-bottom: 1px solid var(--border)">
                <th style="text-align: left; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;" title="Execution phase and what it measures">Phase</th>
                <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;" title="Number of times this phase ran">Runs</th>
                <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;" title="Fastest recorded duration">Min</th>
                <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;" title="Median (p50): half of runs completed within this time. Bar shows proportion relative to the slowest phase.">Median</th>
                <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;" title="Mean (average) duration across all runs for this phase">Mean</th>
                <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;" title="95th percentile: 95% of runs completed within this time. Indicates tail latency.">95th %</th>
                <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;" title="99th percentile: 99% of runs completed within this time. Highlights worst-case outliers.">99th %</th>
                <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;" title="Slowest recorded duration">Max</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="phase in sortedPhaseKeys" :key="phase">
                <td style="padding: 8px 10px;">
                  <div style="font-weight: 500; font-size: 12px;">{{ phaseLabel(phase) }}</div>
                  <div style="font-size: 11px; color: var(--text-muted); margin-top: 2px;">{{ phaseDesc(phase) }}</div>
                </td>
                <td style="padding: 8px 10px; text-align: right; color: var(--text-muted); font-size: 12px;">{{ (data.phases || {})[phase].count }}</td>
                <td style="padding: 8px 10px; text-align: right; color: var(--text-muted); font-size: 12px;">{{ fmtMs((data.phases || {})[phase].min_ms) }}</td>
                <td style="padding: 8px 10px; text-align: right; font-size: 12px;">
                  <div style="font-weight: 600;">{{ fmtMs((data.phases || {})[phase].p50_ms) }}</div>
                  <div
                    v-if="globalMaxMs && (data.phases || {})[phase].p50_ms != null"
                    style="background: var(--border); border-radius: 2px; height: 4px; width: 72px; margin-top: 4px; overflow: hidden;"
                  >
                    <div
                      :style="{
                        background: 'var(--accent)',
                        height: '100%',
                        width: barPct((data.phases || {})[phase].p50_ms) + '%',
                        borderRadius: '2px',
                      }"
                    />
                  </div>
                </td>
                <td style="padding: 8px 10px; text-align: right; font-size: 12px; color: var(--text-muted);">{{ meanMs((data.phases || {})[phase]) }}</td>
                <td
                  :style="'padding: 8px 10px; text-align: right; font-size: 12px; font-weight: 500;' + colorStyleForMs((data.phases || {})[phase].p95_ms)"
                >{{ fmtMs((data.phases || {})[phase].p95_ms) }}</td>
                <td
                  :style="'padding: 8px 10px; text-align: right; font-size: 12px;' + colorStyleForMs((data.phases || {})[phase].p99_ms)"
                >{{ fmtMs((data.phases || {})[phase].p99_ms) }}</td>
                <td style="padding: 8px 10px; text-align: right; color: var(--text-muted); font-size: 12px;">{{ fmtMs((data.phases || {})[phase].max_ms) }}</td>
              </tr>
            </tbody>
          </table>
        </div>

        <div
          style="
            margin-top: 12px;
            padding-top: 10px;
            border-top: 1px solid var(--border);
            font-size: 11px;
            color: var(--text-muted);
            display: flex;
            gap: 14px;
            flex-wrap: wrap;
            align-items: center;
          "
        >
          <span><strong>Median</strong> = typical duration</span>
          <span><strong>95th/99th %</strong> = tail latency</span>
          <span style="display: flex; align-items: center; gap: 4px">
            <span style="display: inline-block; width: 8px; height: 8px; border-radius: 2px; background: #22863a;" />
            &lt;5s
          </span>
          <span style="display: flex; align-items: center; gap: 4px">
            <span style="display: inline-block; width: 8px; height: 8px; border-radius: 2px; background: #d97706;" />
            5–30s
          </span>
          <span style="display: flex; align-items: center; gap: 4px">
            <span style="display: inline-block; width: 8px; height: 8px; border-radius: 2px; background: #dc2626;" />
            &gt;30s
          </span>
        </div>
      </div>
    </div>
  </div>
</template>
