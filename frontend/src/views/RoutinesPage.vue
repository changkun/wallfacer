<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { api } from '../api/client';
import TaskCard from '../components/TaskCard.vue';
import { useDialogStore } from '../stores/dialog';
import type { Task } from '../api/types';

interface FlowOption { slug: string; name: string }

const dialog = useDialogStore();
const routines = ref<Task[]>([]);
const flows = ref<FlowOption[]>([]);
const loading = ref(true);

// Create form.
const prompt = ref('');
const intervalMin = ref(60);
const spawnFlow = ref('brainstorm');
const creating = ref(false);

const INTERVAL_OPTIONS = [1, 5, 15, 30, 60, 180, 360, 720, 1440];

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
    await loadRoutines();
  } catch (e) {
    console.error('create routine:', e);
  } finally {
    creating.value = false;
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
    <header class="routines-head">
      <h1 class="routines-title">Routines</h1>
      <p class="routines-sub">Scheduled cards that spawn fresh tasks on a cadence.</p>
    </header>

    <form class="routine-create" @submit.prevent="createRoutine">
      <textarea
        v-model="prompt"
        class="routine-create__prompt"
        rows="2"
        placeholder="What should this routine do each time it fires?"
      />
      <div class="routine-create__row">
        <label class="routine-create__opt">
          <span>Every</span>
          <select v-model.number="intervalMin" class="routine-create__select">
            <option v-for="m in INTERVAL_OPTIONS" :key="m" :value="m">{{ m }} min</option>
          </select>
        </label>
        <label class="routine-create__opt">
          <span>Flow</span>
          <select v-model="spawnFlow" class="routine-create__select">
            <option v-for="f in flows" :key="f.slug" :value="f.slug">{{ f.name }}</option>
          </select>
        </label>
        <button type="submit" class="routine-create__btn" :disabled="!prompt.trim() || creating">
          {{ creating ? 'Creating…' : 'Create routine' }}
        </button>
      </div>
    </form>

    <div v-if="loading" class="routines-empty">Loading…</div>
    <div v-else-if="!routines.length" class="routines-empty">No routines yet. Create one above.</div>
    <div v-else class="routines-list">
      <div v-for="r in routines" :key="r.id" class="routine-row">
        <TaskCard :task="r" />
        <button type="button" class="routine-delete" title="Delete routine" @click="deleteRoutine(r)">Delete</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.routines-page { max-width: 720px; margin: 0 auto; padding: 24px 16px; }
.routines-title { font-size: 18px; font-weight: 600; margin: 0; color: var(--text); }
.routines-sub { font-size: 13px; color: var(--text-muted); margin: 4px 0 20px; }
.routine-create {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 12px;
  margin-bottom: 24px;
}
.routine-create__prompt {
  width: 100%;
  box-sizing: border-box;
  background: var(--bg-input);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 8px 10px;
  color: var(--text);
  font-size: 13px;
  font-family: var(--font-sans);
  resize: vertical;
  outline: none;
}
.routine-create__row { display: flex; align-items: end; gap: 12px; margin-top: 8px; flex-wrap: wrap; }
.routine-create__opt { display: flex; flex-direction: column; gap: 2px; font-size: 11px; color: var(--text-muted); }
.routine-create__select {
  background: var(--bg-input);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 4px 8px;
  color: var(--text);
  font-size: 12px;
}
.routine-create__btn {
  margin-left: auto;
  background: var(--accent);
  border: 1px solid var(--accent);
  color: #fff;
  border-radius: 6px;
  padding: 6px 14px;
  font-size: 13px;
  cursor: pointer;
}
.routine-create__btn:disabled { opacity: 0.5; cursor: not-allowed; }
.routines-list { display: flex; flex-direction: column; gap: 12px; }
.routine-row { display: flex; align-items: flex-start; gap: 8px; }
.routine-row > :first-child { flex: 1 1 auto; }
.routine-delete {
  font-size: 11px;
  color: var(--text-muted);
  background: none;
  border: 1px solid var(--border);
  border-radius: 5px;
  padding: 4px 8px;
  cursor: pointer;
}
.routine-delete:hover { color: var(--err, #c0392b); border-color: var(--err, #c0392b); }
.routines-empty { color: var(--text-muted); font-size: 13px; text-align: center; padding: 32px; }
</style>
