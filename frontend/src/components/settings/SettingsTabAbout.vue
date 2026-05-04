<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api } from '../../api/client';

interface ContainerCircuit {
  state: string;
  failures: number;
}

interface ActivityStats {
  execs?: number;
  creates?: number;
}

interface WorkerStats {
  enabled?: boolean;
  active_workers?: number;
  creates?: number;
  execs?: number;
  fallbacks?: number;
  by_activity?: Record<string, ActivityStats>;
}

interface TaskStates {
  in_progress?: number;
  waiting?: number;
  backlog?: number;
  done?: number;
  failed?: number;
}

interface RuntimeStatus {
  go_goroutine_count?: number;
  go_heap_alloc_bytes?: number;
  active_containers?: number;
  container_circuit?: ContainerCircuit;
  worker_stats?: WorkerStats;
  task_states?: TaskStates;
}

const status = ref<RuntimeStatus | null>(null);

function formatBytes(bytes: number): string {
  if (bytes < 1024) return bytes + ' B';
  const KB = 1024,
    MB = KB * 1024,
    GB = MB * 1024;
  if (bytes < MB) return (bytes / KB).toFixed(2) + ' KB';
  if (bytes < GB) return (bytes / MB).toFixed(2) + ' MB';
  return (bytes / GB).toFixed(2) + ' GB';
}

function circuitColor(state: string): string {
  return state === 'closed' ? 'var(--text-muted)' : 'var(--accent)';
}

function reuseRatio(ws: WorkerStats): number {
  const execs = ws.execs || 0;
  const fallbacks = ws.fallbacks || 0;
  const total = execs + fallbacks;
  return total > 0 ? Math.round((execs / total) * 100) : 0;
}

function showWorkerExtras(ws: WorkerStats): boolean {
  return (ws.creates || 0) > 0 || (ws.execs || 0) > 0;
}

function activityBreakdown(ws: WorkerStats): string {
  if (!ws.by_activity) return '';
  const parts: string[] = [];
  for (const act in ws.by_activity) {
    const a = ws.by_activity[act];
    let label = act + ': ' + (a.execs || 0) + ' exec';
    if ((a.creates || 0) > 0) {
      label += ' (' + a.creates + ' triggered worker)';
    }
    parts.push(label);
  }
  return parts.join(' · ');
}

function hasActivityBreakdown(ws: WorkerStats): boolean {
  return !!ws.by_activity && Object.keys(ws.by_activity).length > 0;
}

function taskStatesText(ts: TaskStates): string {
  const parts: string[] = [];
  if (ts.in_progress) parts.push(ts.in_progress + ' running');
  if (ts.waiting) parts.push(ts.waiting + ' waiting');
  if (ts.backlog) parts.push(ts.backlog + ' backlog');
  if (ts.done) parts.push(ts.done + ' done');
  if (ts.failed) parts.push(ts.failed + ' failed');
  return parts.join(' · ');
}

function hasTaskStates(ts: TaskStates | undefined): boolean {
  if (!ts) return false;
  return !!(ts.in_progress || ts.waiting || ts.backlog || ts.done || ts.failed);
}

onMounted(async () => {
  try {
    const data = await api<RuntimeStatus>('GET', '/api/debug/runtime');
    status.value = data;
  } catch {
    status.value = null;
  }
});
</script>

<template>
  <div class="settings-tab-content active" data-settings-tab="about">
    <div
      style="
        margin-bottom: 10px;
        font-size: 11px;
        font-weight: 600;
        color: var(--text-muted);
        text-transform: uppercase;
        letter-spacing: 0.5px;
      "
    >
      About
    </div>
    <div
      style="display: flex; align-items: center; gap: 10px; margin-bottom: 12px"
    >
      <div
        style="
          width: 32px;
          height: 32px;
          border-radius: 7px;
          background: linear-gradient(
            135deg,
            #d97757 0%,
            #c4623f 60%,
            #a84e2e 100%
          );
          display: flex;
          align-items: center;
          justify-content: center;
          flex-shrink: 0;
        "
      >
        <span
          style="
            color: white;
            font-size: 15px;
            font-family: 'Instrument Serif', Georgia, serif;
            font-style: italic;
            font-weight: 400;
            line-height: 1;
          "
          >W</span
        >
      </div>
      <div>
        <a
          href="https://github.com/changkun/wallfacer"
          target="_blank"
          rel="noopener noreferrer"
          style="
            font-size: 13px;
            font-weight: 400;
            font-family: 'Instrument Serif', Georgia, serif;
            font-style: italic;
            background: linear-gradient(
              135deg,
              #d97757 0%,
              #c4623f 60%,
              #a84e2e 100%
            );
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
            background-clip: text;
            line-height: 1.3;
            text-decoration: none;
            display: block;
          "
          >Wallfacer</a
        >
        <div style="font-size: 11px; color: var(--text-muted); margin-top: 2px">
          Dispatch AI agents. Collect merged code.
        </div>
      </div>
    </div>
    <div
      style="
        display: flex;
        flex-direction: column;
        gap: 5px;
        font-size: 11px;
        color: var(--text-muted);
      "
    >
      <a
        href="https://github.com/changkun/wallfacer"
        target="_blank"
        rel="noopener noreferrer"
        style="
          display: inline-flex;
          align-items: center;
          gap: 6px;
          color: var(--text-muted);
          text-decoration: none;
          transition: color 0.15s;
        "
      >
        <svg
          width="13"
          height="13"
          viewBox="0 0 24 24"
          fill="currentColor"
          style="flex-shrink: 0; opacity: 0.7"
        >
          <path
            d="M12 0C5.374 0 0 5.373 0 12c0 5.302 3.438 9.8 8.207 11.387.599.111.793-.261.793-.577v-2.234c-3.338.726-4.033-1.416-4.033-1.416-.546-1.387-1.333-1.756-1.333-1.756-1.089-.745.083-.729.083-.729 1.205.084 1.839 1.237 1.839 1.237 1.07 1.834 2.807 1.304 3.492.997.107-.775.418-1.305.762-1.604-2.665-.305-5.467-1.334-5.467-5.931 0-1.311.469-2.381 1.236-3.221-.124-.303-.535-1.524.117-3.176 0 0 1.008-.322 3.301 1.23.957-.266 1.983-.399 3.003-.404 1.02.005 2.047.138 3.006.404 2.291-1.552 3.297-1.23 3.297-1.23.653 1.653.242 2.874.118 3.176.77.84 1.235 1.911 1.235 3.221 0 4.609-2.807 5.624-5.479 5.921.43.372.823 1.102.823 2.222v3.293c0 .319.192.694.801.576C20.566 21.797 24 17.3 24 12c0-6.627-5.373-12-12-12z"
          />
        </svg>
        github.com/changkun/wallfacer
      </a>
      <div style="display: inline-flex; align-items: center; gap: 6px">
        <svg
          width="13"
          height="13"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
          style="flex-shrink: 0; opacity: 0.7"
        >
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
        </svg>
        MIT License &middot; Copyright &copy; 2026
        <a
          href="https://changkun.de"
          target="_blank"
          rel="noopener noreferrer"
          style="color: inherit; text-decoration: none; transition: color 0.15s"
          >Changkun Ou</a
        >
      </div>
    </div>
    <div
      v-if="status"
      style="margin-top: 16px; padding-top: 12px; border-top: 1px solid var(--border);"
    >
      <div
        style="margin-bottom: 8px; font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px;"
      >
        System Status
      </div>
      <div
        style="display: flex; flex-direction: column; gap: 4px; font-size: 11px; color: var(--text-muted);"
      >
        <div>
          Goroutines: <strong>{{ status.go_goroutine_count || 0 }}</strong>
          &middot; Heap:
          <strong>{{ formatBytes(status.go_heap_alloc_bytes || 0) }}</strong>
        </div>
        <div>
          Active containers:
          <strong>{{ status.active_containers || 0 }}</strong>
        </div>
        <div v-if="status.container_circuit">
          Circuit breaker:
          <strong :style="{ color: circuitColor(status.container_circuit.state) }">{{
            status.container_circuit.state
          }}</strong>
          <template v-if="status.container_circuit.failures > 0">
            ({{ status.container_circuit.failures }} failures)
          </template>
        </div>
        <template v-if="status.worker_stats">
          <div>
            Task workers:
            <strong>{{ status.worker_stats.enabled ? 'enabled' : 'disabled' }}</strong>
            &middot; Active:
            <strong>{{ status.worker_stats.active_workers || 0 }}</strong>
            <template v-if="showWorkerExtras(status.worker_stats)">
              &middot; Creates: {{ status.worker_stats.creates || 0 }} &middot;
              Execs: {{ status.worker_stats.execs || 0 }}
              <template v-if="(status.worker_stats.fallbacks || 0) > 0">
                &middot; Fallbacks: {{ status.worker_stats.fallbacks }}
              </template>
              &middot; Reuse:
              <strong>{{ reuseRatio(status.worker_stats) }}%</strong>
            </template>
          </div>
          <div
            v-if="hasActivityBreakdown(status.worker_stats)"
            style="padding-left: 12px;"
          >
            {{ activityBreakdown(status.worker_stats) }}
          </div>
        </template>
        <div v-if="hasTaskStates(status.task_states)">
          Tasks: {{ taskStatesText(status.task_states!) }}
        </div>
      </div>
    </div>
  </div>
</template>
