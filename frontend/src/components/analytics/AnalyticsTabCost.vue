<script setup lang="ts">
import { ref, onMounted, watch, useTemplateRef, nextTick, onBeforeUnmount } from 'vue';
import { useRouter } from 'vue-router';
import { api } from '../../api/client';
import { basename } from '../../lib/workspaceLabel';
import AppSelect from '../AppSelect.vue';
import { chartPalette, watchPalette } from '../../lib/chartPalette';
import { statusPill } from '../../lib/statusPill';

const WINDOW_OPTIONS = [
  { value: 7, label: 'Last 7 days' },
  { value: 30, label: 'Last 30 days' },
  { value: 0, label: 'All time' },
];

const router = useRouter();
// Open a task from a Top Tasks row — deep-link via the hash route handler,
// matching legacy modal-stats' closeStatsModal→openModal hop.
function openTask(id: string) {
  if (id) void router.push({ path: '/', hash: `#${id}` });
}

interface Bucket {
  count?: number;
  input_tokens?: number;
  output_tokens?: number;
  cost_usd?: number;
}

interface WorkspaceBucket extends Bucket {}

interface DailyEntry { date: string; cost_usd: number }

interface AgentSessionTimelineEntry { date: string; cost_usd: number }

interface AgentSessionGroup {
  label?: string;
  paths?: string[];
  round_count?: number;
  usage?: Bucket;
  timeline?: AgentSessionTimelineEntry[];
}

interface TopTask { id: string; title: string; status: string; cost_usd: number }

interface StatsResponse {
  total_cost_usd?: number;
  total_input_tokens?: number;
  total_output_tokens?: number;
  total_cache_tokens?: number;
  by_status?: Record<string, Bucket>;
  by_activity?: Record<string, Bucket>;
  by_workspace?: Record<string, WorkspaceBucket>;
  daily_usage?: DailyEntry[];
  agent_sessions?: Record<string, AgentSessionGroup>;
  top_tasks?: TopTask[];
}

const STATE = { LOADING: 'loading', ERROR: 'error', CONTENT: 'content' } as const;
type State = typeof STATE[keyof typeof STATE];

const state = ref<State>(STATE.LOADING);
const errorMsg = ref('');
const data = ref<StatsResponse | null>(null);
const agentSessionWindowDays = ref(30);
const agentSessionPeriodInitialized = ref(false);
const dailyChart = useTemplateRef<HTMLCanvasElement>('dailyChart');

const ACTIVITY_ORDER = [
  'implementation', 'test', 'refinement', 'title', 'oversight', 'oversight-test',
];

function fmt(n?: number) { return (n || 0).toLocaleString(); }

// Today's share of the busiest day in the window, for the spend tile's bar.
function todayShare(): number {
  const daily = data.value?.daily_usage || [];
  if (!daily.length) return 0;
  const max = Math.max(...daily.map((d) => d.cost_usd || 0));
  const today = new Date().toISOString().slice(0, 10);
  const t = daily.find((d) => d.date === today)?.cost_usd || 0;
  return max > 0 ? Math.round((t / max) * 100) : 0;
}
function windowLabel(): string {
  const days = data.value?.daily_usage?.length || 0;
  return days ? `last ${days} day${days === 1 ? '' : 's'}` : 'no daily data';
}
function fmtCost(c?: number) { return '$' + (c || 0).toFixed(4); }

function sortedStatusKeys() {
  return Object.keys(data.value?.by_status || {}).sort();
}

function sortedActivityKeys() {
  const byActivity = data.value?.by_activity || {};
  const seen: Record<string, boolean> = {};
  const keys = ACTIVITY_ORDER.filter(k => {
    if (byActivity[k]) { seen[k] = true; return true; }
    return false;
  });
  Object.keys(byActivity).sort().forEach(k => { if (!seen[k]) keys.push(k); });
  return keys;
}

function sortedWorkspaceKeys() {
  const m = data.value?.by_workspace || {};
  return Object.keys(m).sort((a, b) => (m[b].cost_usd || 0) - (m[a].cost_usd || 0));
}

function sortedAgentSessionKeys() {
  const m = data.value?.agent_sessions || {};
  return Object.keys(m).sort((a, b) =>
    ((m[b].usage?.cost_usd) || 0) - ((m[a].usage?.cost_usd) || 0));
}

function sparklinePoints(timeline?: AgentSessionTimelineEntry[]) {
  if (!timeline || timeline.length === 0) return '';
  const W = 80, H = 20;
  let max = 0;
  for (const t of timeline) { if ((t.cost_usd || 0) > max) max = t.cost_usd; }
  const points: string[] = [];
  for (let j = 0; j < timeline.length; j++) {
    const x = timeline.length === 1
      ? W / 2
      : (j / (timeline.length - 1)) * (W - 2) + 1;
    const y = max > 0
      ? H - 2 - ((timeline[j].cost_usd || 0) / max) * (H - 4)
      : H / 2;
    points.push(x.toFixed(1) + ',' + y.toFixed(1));
  }
  return points.join(' ');
}

function drawDailyChart() {
  const canvas = dailyChart.value;
  if (!canvas || !canvas.getContext) return;
  const daily = data.value?.daily_usage || [];
  const ctx = canvas.getContext('2d');
  if (!ctx) return;
  const W = 600, H = 120;
  canvas.width = W;
  canvas.height = H;

  const padTop = 8, padBot = 24;
  const chartH = H - padTop - padBot;

  let maxCost = 0;
  daily.forEach(d => { if (d.cost_usd > maxCost) maxCost = d.cost_usd; });

  const today = new Date().toISOString().slice(0, 10);
  const barW = daily.length ? W / daily.length : 0;
  // Past days on the strong rule, today on the accent, labels on the quiet ink.
  const p = chartPalette();
  const barColor = p.rule2;
  const todayColor = p.accent;
  const labelColor = p.ink4;

  ctx.clearRect(0, 0, W, H);

  daily.forEach((d, i) => {
    const bh = maxCost > 0 && d.cost_usd > 0
      ? Math.max(1, (d.cost_usd / maxCost) * chartH)
      : 0;
    const x = i * barW;
    if (bh > 0) {
      ctx.fillStyle = d.date === today ? todayColor : barColor;
      ctx.fillRect(x + 1, padTop + chartH - bh, barW - 2, bh);
    }
    if (i % 5 === 0) {
      const parts = d.date.split('-');
      const label = parts[1] + '-' + parts[2];
      ctx.fillStyle = labelColor;
      ctx.font = '9px sans-serif';
      ctx.textAlign = 'center';
      ctx.fillText(label, x + barW / 2, H - 6);
    }
  });
}

async function fetchAndRender() {
  state.value = STATE.LOADING;
  try {
    const url = agentSessionWindowDays.value > 0
      ? `/api/stats?days=${agentSessionWindowDays.value}`
      : '/api/stats';
    const r = await api<StatsResponse>('GET', url);
    data.value = r;
    state.value = STATE.CONTENT;
    await nextTick();
    drawDailyChart();
  } catch (e) {
    errorMsg.value = e instanceof Error ? e.message : String(e);
    state.value = STATE.ERROR;
  }
}

async function seedAgentSessionPeriod() {
  if (agentSessionPeriodInitialized.value) return;
  agentSessionPeriodInitialized.value = true;
  try {
    const cfg = await api<{ agent_session_window_days?: number }>('GET', '/api/config');
    if (cfg) {
      const n = parseInt(String(cfg.agent_session_window_days), 10);
      if (!Number.isNaN(n) && n >= 0) agentSessionWindowDays.value = n;
    }
  } catch { /* ignore */ }
}

let stopPalette: (() => void) | null = null;
onMounted(async () => {
  stopPalette = watchPalette(drawDailyChart);
  await seedAgentSessionPeriod();
  fetchAndRender();
});
onBeforeUnmount(() => { stopPalette?.(); });

watch(agentSessionWindowDays, () => fetchAndRender());
</script>

<template>
  <div class="an-body">
    <div v-if="state === 'loading'" class="an-state">Loading…</div>
    <div v-else-if="state === 'error'" class="an-error">{{ errorMsg }}</div>
    <template v-else-if="state === 'content' && data">
      <div class="an-tiles">
        <div class="card an-tile">
          <span class="eyebrow">Total cost</span>
          <span class="an-tile__num">{{ fmtCost(data.total_cost_usd) }}</span>
          <span class="an-tile__sub">Today is {{ todayShare() }}% of the busiest day, {{ windowLabel() }}</span>
          <span class="bar"><i :style="{ width: todayShare() + '%' }"></i></span>
        </div>
        <div class="card an-tile">
          <span class="eyebrow">Input tokens</span>
          <span class="an-tile__num">{{ fmt(data.total_input_tokens) }}</span>
          <span class="an-tile__sub">Prompt and context sent to the model</span>
        </div>
        <div class="card an-tile">
          <span class="eyebrow">Output tokens</span>
          <span class="an-tile__num">{{ fmt(data.total_output_tokens) }}</span>
          <span class="an-tile__sub">Generated by the model</span>
        </div>
        <div class="card an-tile">
          <span class="eyebrow">Cache tokens</span>
          <span class="an-tile__num">{{ fmt(data.total_cache_tokens) }}</span>
          <span class="an-tile__sub">Served from the prompt cache</span>
        </div>
      </div>

      <section class="card an-section">
        <div class="card-head"><span class="eyebrow">Daily spend</span><span class="muted">{{ windowLabel() }}</span></div>
        <div class="an-section__body">
          <canvas ref="dailyChart" class="an-chart" width="600" height="120" />
        </div>
      </section>

      <section class="card an-section">
        <div class="card-head"><span class="eyebrow">By status</span></div>
        <div class="an-table-wrap">
          <table class="an-table">
            <thead>
              <tr><th>Status</th><th class="num">Input tokens</th><th class="num">Output tokens</th><th class="num">Cost USD</th></tr>
            </thead>
            <tbody>
              <tr v-for="k in sortedStatusKeys()" :key="k">
                <td><span class="pill" :class="statusPill(k)">{{ k.replace('_', ' ') }}</span></td>
                <td class="num">{{ fmt((data.by_status || {})[k].input_tokens) }}</td>
                <td class="num">{{ fmt((data.by_status || {})[k].output_tokens) }}</td>
                <td class="num strong">{{ fmtCost((data.by_status || {})[k].cost_usd) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="card an-section">
        <div class="card-head"><span class="eyebrow">By activity</span></div>
        <div class="an-table-wrap">
          <table class="an-table">
            <thead>
              <tr><th>Activity</th><th class="num">Input tokens</th><th class="num">Output tokens</th><th class="num">Cost USD</th></tr>
            </thead>
            <tbody>
              <tr v-for="k in sortedActivityKeys()" :key="k">
                <td class="strong">{{ k }}</td>
                <td class="num">{{ fmt((data.by_activity || {})[k].input_tokens) }}</td>
                <td class="num">{{ fmt((data.by_activity || {})[k].output_tokens) }}</td>
                <td class="num strong">{{ fmtCost((data.by_activity || {})[k].cost_usd) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-if="sortedWorkspaceKeys().length" class="card an-section">
        <div class="card-head"><span class="eyebrow">By workspace</span></div>
        <div class="an-table-wrap">
          <table class="an-table">
            <thead>
              <tr><th>Workspace</th><th class="num">Tasks</th><th class="num">Input tokens</th><th class="num">Output tokens</th><th class="num">Cost USD</th></tr>
            </thead>
            <tbody>
              <tr v-for="path in sortedWorkspaceKeys()" :key="path">
                <td class="strong"><span class="clip" :title="path">{{ basename(path) }}</span></td>
                <td class="num">{{ fmt((data.by_workspace || {})[path].count) }}</td>
                <td class="num">{{ fmt((data.by_workspace || {})[path].input_tokens) }}</td>
                <td class="num">{{ fmt((data.by_workspace || {})[path].output_tokens) }}</td>
                <td class="num strong">{{ fmtCost((data.by_workspace || {})[path].cost_usd) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section v-if="sortedAgentSessionKeys().length" class="card an-section">
        <div class="card-head">
          <span class="eyebrow">Agent sessions</span>
          <label class="an-tools">
            Window
            <AppSelect
              v-model="agentSessionWindowDays"
              :options="WINDOW_OPTIONS"
              aria-label="Agent session cost window"
            />
          </label>
        </div>
        <div class="an-table-wrap">
          <table class="an-table">
            <thead>
              <tr><th>Group</th><th class="num">Rounds</th><th class="num">Input tokens</th><th class="num">Output tokens</th><th class="num">Cost USD</th><th class="num">Trend</th></tr>
            </thead>
            <tbody>
              <tr v-for="key in sortedAgentSessionKeys()" :key="key">
                <td class="strong">
                  <span :title="((data.agent_sessions || {})[key].paths || []).join('\n') || key">{{ (data.agent_sessions || {})[key].label || key }}</span>
                </td>
                <td class="num">{{ fmt((data.agent_sessions || {})[key].round_count) }}</td>
                <td class="num">{{ fmt((data.agent_sessions || {})[key].usage?.input_tokens) }}</td>
                <td class="num">{{ fmt((data.agent_sessions || {})[key].usage?.output_tokens) }}</td>
                <td class="num strong">{{ fmtCost((data.agent_sessions || {})[key].usage?.cost_usd) }}</td>
                <td class="num">
                  <svg
                    v-if="((data.agent_sessions || {})[key].timeline || []).length"
                    class="an-spark"
                    width="80"
                    height="20"
                    viewBox="0 0 80 20"
                    aria-hidden="true"
                  >
                    <polyline :points="sparklinePoints((data.agent_sessions || {})[key].timeline)" />
                  </svg>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="card an-section">
        <div class="card-head"><span class="eyebrow">Top 10 tasks by cost</span></div>
        <div class="an-table-wrap">
          <table class="an-table">
            <thead>
              <tr><th>Title</th><th>Status</th><th class="num">Cost USD</th></tr>
            </thead>
            <tbody>
              <tr
                v-for="t in (data.top_tasks || [])"
                :key="t.id"
                class="clickable top-task-row"
                @click="openTask(t.id)"
              >
                <td><span class="clip link">{{ t.title }}</span></td>
                <td><span class="pill" :class="statusPill(t.status)">{{ t.status.replace('_', ' ') }}</span></td>
                <td class="num strong">{{ fmtCost(t.cost_usd) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </template>
  </div>
</template>
