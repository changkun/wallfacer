<script setup lang="ts">
// Routines are scheduled cards that spawn a fresh task on a cadence. The page
// is one reading column: a create card, then a card of rows, one per routine,
// with the schedule as a pill, the countdown, the enabled switch, the
// interval, Run now and Delete. The schedule endpoint accepts the interval
// and the enabled flag; the prompt and the agent graph are fixed at creation.
import { ref, computed, onMounted } from 'vue';
import { api } from '../api/client';
import AppSelect from '../components/AppSelect.vue';
import SettingToggle from '../components/settings/SettingToggle.vue';
import { useDialogStore } from '../stores/dialog';
import { useToastStore } from '../stores/toast';
import { useNow } from '../composables/useNow';
import { routineCountdown, routineLastFired, routineMinutes } from '../lib/routineTime';
import type { Task } from '../api/types';
import '../styles/routines.css';

interface FlowOption { slug: string; name: string }

const dialog = useDialogStore();
const toast = useToastStore();
const now = useNow();
const routines = ref<Task[]>([]);
const flows = ref<FlowOption[]>([]);
const loading = ref(true);
const busy = ref<Record<string, boolean>>({});

const prompt = ref('');
const intervalMin = ref(60);
const spawnFlow = ref('implement');
const creating = ref(false);

const BRAINSTORM_EXAMPLE =
  'Review the repository for the three highest-impact improvements and create a task for each.';

function fillExample() {
  prompt.value = BRAINSTORM_EXAMPLE;
}

const INTERVAL_OPTIONS = [1, 5, 15, 30, 60, 180, 360, 720, 1440];
const intervalOptions = INTERVAL_OPTIONS.map((m) => ({ value: m, label: `${m} min` }));
const flowOptions = computed(() => flows.value.map((f) => ({ value: f.slug, label: f.name })));

// A routine whose interval is not in the fixed list still shows its own value.
function intervalOptionsFor(r: Task) {
  const set = new Set(INTERVAL_OPTIONS);
  const m = routineMinutes(r);
  if (m > 0) set.add(m);
  return [...set].sort((a, b) => a - b).map((v) => ({ value: v, label: `${v} min` }));
}

function spawnLabel(r: Task): string {
  const slug = r.routine_spawn_flow || r.routine_spawn_kind || 'task';
  return flows.value.find((f) => f.slug === slug)?.name || slug;
}

const promptEl = ref<HTMLTextAreaElement | null>(null);

function autogrow() {
  const el = promptEl.value;
  if (!el) return;
  el.style.height = 'auto';
  el.style.height = `${el.scrollHeight}px`;
}

async function loadRoutines() {
  loading.value = true;
  try {
    const res = await api<{ routines: Task[] }>('GET', '/api/routines');
    routines.value = res?.routines ?? [];
  } catch (e) {
    console.error('load routines:', e);
  } finally {
    loading.value = false;
  }
}

async function loadFlows() {
  try {
    const res = await api<FlowOption[] | { flows: FlowOption[] }>('GET', '/api/flows');
    flows.value = Array.isArray(res) ? res : (res?.flows ?? []);
  } catch (e) {
    console.error('load flows:', e);
  }
}

async function createRoutine() {
  const text = prompt.value.trim();
  if (!text || creating.value) return;
  creating.value = true;
  try {
    await api('POST', '/api/routines', {
      prompt: text,
      interval_minutes: intervalMin.value,
      spawn_flow: spawnFlow.value || undefined,
      enabled: true,
    });
    prompt.value = '';
    autogrow();
    await loadRoutines();
  } catch (e) {
    console.error('create routine:', e);
    toast.push(`Create failed: ${e instanceof Error ? e.message : String(e)}`, { kind: 'error' });
  } finally {
    creating.value = false;
  }
}

async function schedule(r: Task, patch: { interval_minutes?: number; enabled?: boolean }) {
  if (busy.value[r.id]) return;
  busy.value = { ...busy.value, [r.id]: true };
  try {
    await api('PATCH', `/api/routines/${r.id}/schedule`, patch);
    await loadRoutines();
  } catch (e) {
    console.error('update routine:', e);
    toast.push(`Update failed: ${e instanceof Error ? e.message : String(e)}`, { kind: 'error' });
  } finally {
    busy.value = { ...busy.value, [r.id]: false };
  }
}

function setEnabled(r: Task, enabled: boolean) {
  void schedule(r, { enabled });
}

function setInterval(r: Task, minutes: number) {
  if (!Number.isFinite(minutes) || minutes < 1 || minutes === routineMinutes(r)) return;
  void schedule(r, { interval_minutes: minutes });
}

async function runNow(r: Task) {
  if (busy.value[r.id]) return;
  busy.value = { ...busy.value, [r.id]: true };
  try {
    await api('POST', `/api/routines/${r.id}/trigger`);
    toast.push('Routine fired', { kind: 'success' });
    await loadRoutines();
  } catch (e) {
    console.error('trigger routine:', e);
    toast.push(`Run failed: ${e instanceof Error ? e.message : String(e)}`, { kind: 'error' });
  } finally {
    busy.value = { ...busy.value, [r.id]: false };
  }
}

async function deleteRoutine(r: Task) {
  const ok = await dialog.confirm({
    title: 'Delete routine',
    message: `Delete the routine "${r.title || r.prompt}"? It stops firing and is removed.`,
    confirmLabel: 'Delete',
    danger: true,
  });
  if (!ok) return;
  try {
    await api('DELETE', `/api/tasks/${r.id}`);
    await loadRoutines();
  } catch (e) {
    console.error('delete routine:', e);
  }
}

onMounted(() => { loadRoutines(); loadFlows(); });
</script>

<template>
  <div class="routines-page">
    <div class="routines-page__inner">
      <header class="routines-head">
        <h1 class="routines-title">Routines</h1>
        <p class="routines-sub">Scheduled cards that spawn fresh tasks on a cadence.</p>
      </header>

      <form class="card routine-create" @submit.prevent="createRoutine">
        <div class="card-pad routine-create__body">
          <textarea
            ref="promptEl"
            v-model="prompt"
            class="field routine-create__prompt"
            rows="2"
            placeholder="What should this routine do each time it fires?"
            @input="autogrow"
          />
          <div class="routine-create__row">
            <label class="routine-create__opt">
              <span class="eyebrow">Every</span>
              <AppSelect v-model="intervalMin" :options="intervalOptions" class="routine-create__select" aria-label="Interval" />
            </label>
            <label class="routine-create__opt">
              <span class="eyebrow">Agent graph</span>
              <AppSelect v-model="spawnFlow" :options="flowOptions" class="routine-create__select" aria-label="Agent graph" />
            </label>
            <button
              type="button"
              class="btn sm ghost routine-create__example"
              aria-label="Use an example brainstorm prompt"
              @click="fillExample"
            >Try an example</button>
            <button type="submit" class="btn routine-create__btn" :disabled="!prompt.trim() || creating">
              {{ creating ? 'Creating…' : 'Create routine' }}
            </button>
          </div>
        </div>
      </form>

      <div v-if="loading" class="routines-loading">
        <span class="spinner" aria-hidden="true"></span>
        <span class="wf-shimmer-text">Loading routines…</span>
      </div>
      <div v-else-if="!routines.length" class="routines-empty">
        <span class="eyebrow">No routines yet</span>
        <p>Create one above. Each routine spawns a task on its schedule.</p>
      </div>
      <div v-else class="card compact routines-list">
        <div class="rows">
          <div v-for="r in routines" :key="r.id" class="row routine-row" :data-routine-id="r.id">
            <div class="row-main">
              <div class="row-title routine-row__title" :title="r.prompt">{{ r.title || r.prompt }}</div>
              <div class="row-meta routine-row__meta">
                <span class="pill pill-neutral routine-row__every">every {{ routineMinutes(r) }} min</span>
                <span class="routine-row__flow">{{ spawnLabel(r) }}</span>
                <span class="muted routine-row__next">{{ routineCountdown(r, now) }}</span>
                <span v-if="routineLastFired(r, now)" class="muted">{{ routineLastFired(r, now) }}</span>
              </div>
            </div>
            <div class="row-end routine-row__end">
              <AppSelect
                :model-value="routineMinutes(r)"
                :options="intervalOptionsFor(r)"
                class="routine-row__interval"
                aria-label="Interval"
                @update:model-value="(v) => setInterval(r, Number(v))"
              />
              <SettingToggle
                :model-value="!!r.routine_enabled"
                :disabled="!!busy[r.id]"
                label="Enabled"
                @update:model-value="(v) => setEnabled(r, v)"
              />
              <button type="button" class="btn sm ghost routine-row__run" :disabled="!!busy[r.id]" @click="runNow(r)">Run now</button>
              <button type="button" class="icon-btn sm danger routine-row__delete" title="Delete routine" aria-label="Delete routine" @click="deleteRoutine(r)">
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" /></svg>
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
