<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import { api } from '../api/client';
import TaskCard from '../components/TaskCard.vue';
import AppSelect from '../components/AppSelect.vue';
import { useDialogStore } from '../stores/dialog';
import type { Task } from '../api/types';
import '../styles/routines.css';

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
const intervalOptions = INTERVAL_OPTIONS.map((m) => ({ value: m, label: `${m} min` }));
const flowOptions = computed(() => flows.value.map((f) => ({ value: f.slug, label: f.name })));

const promptEl = ref<HTMLTextAreaElement | null>(null);

// Grow the textarea to fit content, capped by its CSS max-height.
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
  <div v-if="loading" class="routines-page">
    <div class="routines-loading">Loading…</div>
  </div>

  <!-- Empty: centered hero with the create form. -->
  <div v-else-if="!routines.length" class="routines-page routines-page--empty">
    <div class="routines-page__inner">
      <div class="routines-hero">
        <span class="routines-hero__icon" aria-hidden="true">
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="12" cy="12" r="9"></circle>
            <polyline points="12 7 12 12 15 14"></polyline>
          </svg>
        </span>
        <h1 class="routines-hero__title">No routines yet</h1>
        <p class="routines-hero__sub">Schedule a card to spawn a fresh task on a cadence.</p>
        <form class="routine-create" @submit.prevent="createRoutine">
          <textarea
            ref="promptEl"
            v-model="prompt"
            class="routine-create__prompt"
            rows="2"
            placeholder="What should this routine do each time it fires?"
            @input="autogrow"
          />
          <div class="routine-create__row">
            <label class="routine-create__opt">
              <span>Every</span>
              <AppSelect v-model="intervalMin" :options="intervalOptions" class="routine-create__select" aria-label="Interval" />
            </label>
            <label class="routine-create__opt">
              <span>Flow</span>
              <AppSelect v-model="spawnFlow" :options="flowOptions" class="routine-create__select" aria-label="Flow" />
            </label>
            <button type="submit" class="btn btn-accent routine-create__btn" :disabled="!prompt.trim() || creating">
              {{ creating ? 'Creating…' : 'Create routine' }}
            </button>
          </div>
        </form>
      </div>
    </div>
  </div>

  <!-- Populated: header, compact form, then the list. -->
  <div v-else class="routines-page">
    <div class="routines-page__inner">
      <header class="routines-head">
        <h1 class="routines-title">Routines</h1>
        <p class="routines-sub">Scheduled cards that spawn fresh tasks on a cadence.</p>
      </header>

      <form class="routine-create" @submit.prevent="createRoutine">
        <textarea
          ref="promptEl"
          v-model="prompt"
          class="routine-create__prompt"
          rows="2"
          placeholder="What should this routine do each time it fires?"
          @input="autogrow"
        />
        <div class="routine-create__row">
          <label class="routine-create__opt">
            <span>Every</span>
            <AppSelect v-model="intervalMin" :options="intervalOptions" class="routine-create__select" aria-label="Interval" />
          </label>
          <label class="routine-create__opt">
            <span>Flow</span>
            <AppSelect v-model="spawnFlow" :options="flowOptions" class="routine-create__select" aria-label="Flow" />
          </label>
          <button type="submit" class="btn btn-accent routine-create__btn" :disabled="!prompt.trim() || creating">
            {{ creating ? 'Creating…' : 'Create routine' }}
          </button>
        </div>
      </form>

      <div class="routines-list">
        <div v-for="r in routines" :key="r.id" class="routine-row">
          <TaskCard :task="r" />
          <button type="button" class="routine-delete" title="Delete routine" @click="deleteRoutine(r)">Delete</button>
        </div>
      </div>
    </div>
  </div>
</template>
