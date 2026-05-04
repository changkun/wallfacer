<script setup lang="ts">
import { ref, onMounted, watch, useTemplateRef, nextTick } from 'vue';
import { api } from '../../api/client';

interface Bucket {
  count?: number;
  input_tokens?: number;
  output_tokens?: number;
  cost_usd?: number;
}

interface WorkspaceBucket extends Bucket {}

interface DailyEntry { date: string; cost_usd: number }

interface PlanningTimelineEntry { date: string; cost_usd: number }

interface PlanningGroup {
  label?: string;
  paths?: string[];
  round_count?: number;
  usage?: Bucket;
  timeline?: PlanningTimelineEntry[];
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
  planning?: Record<string, PlanningGroup>;
  top_tasks?: TopTask[];
}

const STATE = { LOADING: 'loading', ERROR: 'error', CONTENT: 'content' } as const;
type State = typeof STATE[keyof typeof STATE];

const state = ref<State>(STATE.LOADING);
const errorMsg = ref('');
const data = ref<StatsResponse | null>(null);
const planningWindowDays = ref(30);
const planningPeriodInitialized = ref(false);
const dailyChart = useTemplateRef<HTMLCanvasElement>('dailyChart');

const ACTIVITY_ORDER = [
  'implementation', 'test', 'refinement', 'title', 'oversight', 'oversight-test',
];

function fmt(n?: number) { return (n || 0).toLocaleString(); }
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

function workspaceLabel(p: string) {
  const parts = p.replace(/\\/g, '/').split('/');
  return parts[parts.length - 1] || p;
}

function sortedPlanningKeys() {
  const m = data.value?.planning || {};
  return Object.keys(m).sort((a, b) =>
    ((m[b].usage?.cost_usd) || 0) - ((m[a].usage?.cost_usd) || 0));
}

function sparklinePoints(timeline?: PlanningTimelineEntry[]) {
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
  const isDark = document.documentElement.getAttribute('data-theme') === 'dark';
  const barColor = isDark ? '#475569' : '#94a3b8';
  const todayColor = '#3b82f6';
  const labelColor = isDark ? '#64748b' : '#94a3b8';

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
    const url = planningWindowDays.value > 0
      ? `/api/stats?days=${planningWindowDays.value}`
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

async function seedPlanningPeriod() {
  if (planningPeriodInitialized.value) return;
  planningPeriodInitialized.value = true;
  try {
    const cfg = await api<{ planning_window_days?: number }>('GET', '/api/config');
    if (cfg) {
      const n = parseInt(String(cfg.planning_window_days), 10);
      if (!Number.isNaN(n) && n >= 0) planningWindowDays.value = n;
    }
  } catch { /* ignore */ }
}

onMounted(async () => {
  await seedPlanningPeriod();
  fetchAndRender();
});

watch(planningWindowDays, () => fetchAndRender());
</script>

<template>
  <div>
    <div
      style="
        display: flex;
        align-items: flex-start;
        justify-content: space-between;
        margin-bottom: 16px;
        gap: 12px;
      "
    >
      <div>
        <h3 style="font-size: 16px; font-weight: 600; margin: 0">Tokens &amp; cost</h3>
        <div style="font-size: 12px; color: var(--text-muted); margin-top: 3px">
          Aggregate token usage and dollar cost across tasks, activities, workspaces, and planning rounds.
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
      <div v-else-if="state === 'content' && data">
        <div>
          <div style="display: flex; gap: 24px; flex-wrap: wrap; padding: 4px 0 20px;">
            <div>
              <div style="font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;margin-bottom:3px;">Total Cost</div>
              <div style="font-size:22px;font-weight:600;">{{ fmtCost(data.total_cost_usd) }}</div>
            </div>
            <div>
              <div style="font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;margin-bottom:3px;">Input Tokens</div>
              <div style="font-size:22px;font-weight:600;">{{ fmt(data.total_input_tokens) }}</div>
            </div>
            <div>
              <div style="font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;margin-bottom:3px;">Output Tokens</div>
              <div style="font-size:22px;font-weight:600;">{{ fmt(data.total_output_tokens) }}</div>
            </div>
            <div>
              <div style="font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;margin-bottom:3px;">Cache Tokens</div>
              <div style="font-size:22px;font-weight:600;">{{ fmt(data.total_cache_tokens) }}</div>
            </div>
          </div>
        </div>

        <div style="margin-bottom: 20px">
          <div style="font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 8px;">
            Daily Spend (last 30 days)
          </div>
          <canvas
            ref="dailyChart"
            width="600"
            height="120"
            style="width: 100%; max-width: 600px; height: 120px; display: block;"
          />
        </div>

        <div style="margin-bottom: 20px">
          <div style="font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 8px;">By Status</div>
          <div style="overflow-x: auto">
            <table style="width: 100%; border-collapse: collapse; font-size: 12px">
              <thead>
                <tr style="border-bottom: 1px solid var(--border)">
                  <th style="text-align: left; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px;">Status</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Input Tokens</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Output Tokens</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Cost USD</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="k in sortedStatusKeys()" :key="k">
                  <td style="padding: 6px 10px; font-weight: 500;">{{ k }}</td>
                  <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmt((data.by_status || {})[k].input_tokens) }}</td>
                  <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmt((data.by_status || {})[k].output_tokens) }}</td>
                  <td style="padding: 6px 10px; text-align: right; font-weight: 500;">{{ fmtCost((data.by_status || {})[k].cost_usd) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div style="margin-bottom: 20px">
          <div style="font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 8px;">By Activity</div>
          <div style="overflow-x: auto">
            <table style="width: 100%; border-collapse: collapse; font-size: 12px">
              <thead>
                <tr style="border-bottom: 1px solid var(--border)">
                  <th style="text-align: left; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px;">Activity</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Input Tokens</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Output Tokens</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Cost USD</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="k in sortedActivityKeys()" :key="k">
                  <td style="padding: 6px 10px; font-weight: 500;">{{ k }}</td>
                  <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmt((data.by_activity || {})[k].input_tokens) }}</td>
                  <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmt((data.by_activity || {})[k].output_tokens) }}</td>
                  <td style="padding: 6px 10px; text-align: right; font-weight: 500;">{{ fmtCost((data.by_activity || {})[k].cost_usd) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div v-if="sortedWorkspaceKeys().length" style="margin-bottom: 20px">
          <div style="font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 8px;">By Workspace</div>
          <div style="overflow-x: auto">
            <table style="width: 100%; border-collapse: collapse; font-size: 12px">
              <thead>
                <tr style="border-bottom: 1px solid var(--border)">
                  <th style="text-align: left; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px;">Workspace</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Tasks</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Input Tokens</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Output Tokens</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Cost USD</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="path in sortedWorkspaceKeys()" :key="path">
                  <td style="padding: 6px 10px; font-weight: 500;">
                    <span :title="path" style="cursor: default;">{{ workspaceLabel(path) }}</span>
                  </td>
                  <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmt((data.by_workspace || {})[path].count) }}</td>
                  <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmt((data.by_workspace || {})[path].input_tokens) }}</td>
                  <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmt((data.by_workspace || {})[path].output_tokens) }}</td>
                  <td style="padding: 6px 10px; text-align: right; font-weight: 500;">{{ fmtCost((data.by_workspace || {})[path].cost_usd) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div v-if="sortedPlanningKeys().length" style="margin-bottom: 20px">
          <div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px;">
            <div style="font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px;">Planning</div>
            <label style="font-size: 11px; color: var(--text-muted); display: flex; align-items: center; gap: 6px;">
              Window
              <select
                v-model.number="planningWindowDays"
                style="font-size: 11px; padding: 2px 6px; border-radius: 4px; border: 1px solid var(--border); background: var(--bg); color: inherit;"
              >
                <option :value="7">Last 7 days</option>
                <option :value="30">Last 30 days</option>
                <option :value="0">All time</option>
              </select>
            </label>
          </div>
          <div style="overflow-x: auto">
            <table style="width: 100%; border-collapse: collapse; font-size: 12px">
              <thead>
                <tr style="border-bottom: 1px solid var(--border)">
                  <th style="text-align: left; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px;">Group</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Rounds</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Input Tokens</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Output Tokens</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Cost USD</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Trend</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="key in sortedPlanningKeys()" :key="key">
                  <td style="padding: 6px 10px; font-weight: 500;">
                    <span
                      :title="((data.planning || {})[key].paths || []).join('\n') || key"
                      style="cursor: default;"
                    >{{ (data.planning || {})[key].label || key }}</span>
                  </td>
                  <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmt((data.planning || {})[key].round_count) }}</td>
                  <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmt((data.planning || {})[key].usage?.input_tokens) }}</td>
                  <td style="padding: 6px 10px; text-align: right; color: var(--text-muted);">{{ fmt((data.planning || {})[key].usage?.output_tokens) }}</td>
                  <td style="padding: 6px 10px; text-align: right; font-weight: 500;">{{ fmtCost((data.planning || {})[key].usage?.cost_usd) }}</td>
                  <td style="padding: 6px 10px; text-align: right;">
                    <svg
                      v-if="((data.planning || {})[key].timeline || []).length"
                      width="80"
                      height="20"
                      viewBox="0 0 80 20"
                      style="display: block;"
                    >
                      <polyline
                        fill="none"
                        stroke="var(--accent,#3b82f6)"
                        stroke-width="1.5"
                        :points="sparklinePoints((data.planning || {})[key].timeline)"
                      />
                    </svg>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>

        <div>
          <div style="font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px; margin-bottom: 8px;">Top 10 Tasks by Cost</div>
          <div style="overflow-x: auto">
            <table style="width: 100%; border-collapse: collapse; font-size: 12px">
              <thead>
                <tr style="border-bottom: 1px solid var(--border)">
                  <th style="text-align: left; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px;">Title</th>
                  <th style="text-align: left; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px;">Status</th>
                  <th style="text-align: right; padding: 6px 10px; font-weight: 600; color: var(--text-muted); font-size: 11px; text-transform: uppercase; letter-spacing: 0.4px; white-space: nowrap;">Cost USD</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="t in (data.top_tasks || [])" :key="t.id">
                  <td style="padding: 6px 10px; max-width: 360px;">
                    <span style="display: block; max-width: 360px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ t.title }}</span>
                  </td>
                  <td style="padding: 6px 10px; color: var(--text-muted); white-space: nowrap;">{{ t.status }}</td>
                  <td style="padding: 6px 10px; text-align: right; font-weight: 500; white-space: nowrap;">{{ fmtCost(t.cost_usd) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
