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
  container_run: { label: 'Harness Run', desc: 'Full harness process lifecycle from start to exit' },
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

// Tail latency reads on the ramp: under 5s ok, under 30s warn, above err.
function toneForMs(ms?: number) {
  if (ms == null) return '';
  if (ms < 5000) return 'an-ok';
  if (ms < 30000) return 'an-warn';
  return 'an-err';
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
  <div class="an-body">
    <div v-if="state === 'loading'" class="an-state">Loading…</div>
    <div v-else-if="state === 'error'" class="an-error">{{ errorMsg }}</div>
    <div v-else-if="state === 'empty'" class="an-state">No span data yet. Run a task to collect timing data.</div>
    <template v-else-if="state === 'table' && data">
      <div class="an-tiles">
        <div v-for="tile in tiles" :key="tile.label" class="card an-tile">
          <span class="eyebrow">{{ tile.label }}</span>
          <span class="an-tile__num">{{ tile.value }}</span>
        </div>
        <div v-if="(data.throughput?.daily_completions || []).length" class="card an-tile an-tile--wide">
          <span class="eyebrow">Daily completions</span>
          <div class="an-bars">
            <div
              v-for="d in (data.throughput?.daily_completions || [])"
              :key="d.date"
              class="an-bars__col"
              :title="d.date + ': ' + d.count"
            >
              <div class="an-bar" :class="{ 'an-bar--on': d.count > 0 }" :style="{ height: dailyBarHeight(d.count) + '%' }" />
            </div>
          </div>
          <span class="an-tile__sub">Last 30 days</span>
        </div>
      </div>

      <p class="an-note">
        <strong>{{ data.tasks_scanned }}</strong> tasks scanned,
        <strong>{{ data.spans_total }}</strong> spans across
        <strong>{{ sortedPhaseKeys.length }}</strong> phase{{ sortedPhaseKeys.length === 1 ? '' : 's' }}.
        Per-phase latency is aggregated across all tasks.
      </p>

      <section class="card an-section">
        <div class="card-head"><span class="eyebrow">Phases</span></div>
        <div class="an-table-wrap">
          <table class="an-table">
            <thead>
              <tr>
                <th title="Execution phase and what it measures">Phase</th>
                <th class="num" title="Number of times this phase ran">Runs</th>
                <th class="num" title="Fastest recorded duration">Min</th>
                <th class="num" title="Median (p50): half of runs completed within this time. Bar shows proportion relative to the slowest phase.">Median</th>
                <th class="num" title="Mean (average) duration across all runs for this phase">Mean</th>
                <th class="num" title="95th percentile: 95% of runs completed within this time. Indicates tail latency.">95th %</th>
                <th class="num" title="99th percentile: 99% of runs completed within this time. Highlights worst-case outliers.">99th %</th>
                <th class="num" title="Slowest recorded duration">Max</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="phase in sortedPhaseKeys" :key="phase">
                <td>
                  <span class="strong">{{ phaseLabel(phase) }}</span>
                  <span class="sub">{{ phaseDesc(phase) }}</span>
                </td>
                <td class="num">{{ (data.phases || {})[phase].count }}</td>
                <td class="num">{{ fmtMs((data.phases || {})[phase].min_ms) }}</td>
                <td class="num strong">
                  {{ fmtMs((data.phases || {})[phase].p50_ms) }}
                  <span v-if="globalMaxMs && (data.phases || {})[phase].p50_ms != null" class="an-track">
                    <i :style="{ width: barPct((data.phases || {})[phase].p50_ms) + '%' }"></i>
                  </span>
                </td>
                <td class="num">{{ meanMs((data.phases || {})[phase]) }}</td>
                <td class="num strong" :class="toneForMs((data.phases || {})[phase].p95_ms)">{{ fmtMs((data.phases || {})[phase].p95_ms) }}</td>
                <td class="num" :class="toneForMs((data.phases || {})[phase].p99_ms)">{{ fmtMs((data.phases || {})[phase].p99_ms) }}</td>
                <td class="num">{{ fmtMs((data.phases || {})[phase].max_ms) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="an-legend">
          <span><strong>Median</strong> = typical duration</span>
          <span><strong>95th/99th %</strong> = tail latency</span>
          <span class="an-legend__item"><span class="an-swatch an-swatch--ok"></span>&lt;5s</span>
          <span class="an-legend__item"><span class="an-swatch an-swatch--warn"></span>5–30s</span>
          <span class="an-legend__item"><span class="an-swatch an-swatch--err"></span>&gt;30s</span>
        </div>
      </section>
    </template>
  </div>
</template>
