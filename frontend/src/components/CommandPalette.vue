<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue';
import { useRouter, useRoute } from 'vue-router';
import { api } from '../api/client';
import { useTaskStore } from '../stores/tasks';
import type { Task } from '../api/types';

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  select: [taskId: string];
}>();

const router = useRouter();
const route = useRoute();
const taskStore = useTaskStore();

const query = ref('');
const activeIndex = ref(0);
const inputRef = ref<HTMLInputElement | null>(null);
const remoteResults = ref<Task[]>([]);

let serverSeq = 0;
let serverTimer: ReturnType<typeof setTimeout> | null = null;

interface FuzzyResult {
  matched: boolean;
  score: number;
}

function fuzzyMatch(text: string, q: string): FuzzyResult {
  const hay = String(text || '').toLowerCase();
  const needle = String(q || '').trim().toLowerCase();
  if (!needle) return { matched: true, score: Number.MAX_SAFE_INTEGER };
  const exact = hay.indexOf(needle);
  if (exact !== -1) return { matched: true, score: 10_000 - exact };
  let position = 0;
  let gap = 0;
  for (let i = 0; i < needle.length; i++) {
    const ch = needle[i];
    const next = hay.indexOf(ch, position);
    if (next === -1) return { matched: false, score: 0 };
    gap += next - position;
    position = next + 1;
  }
  return { matched: true, score: 1_000 - gap };
}

function shortId(t: Task): string {
  return t.id ? t.id.slice(0, 8) : '';
}

function taskTitle(t: Task): string {
  return t.title || t.prompt || '';
}

function matchTask(task: Task, q: string): FuzzyResult | null {
  const fields = [taskTitle(task), task.prompt || '', shortId(task)];
  let best: FuzzyResult | null = null;
  for (const f of fields) {
    const r = fuzzyMatch(f, q);
    if (!r.matched) continue;
    if (!best || r.score > best.score) best = r;
  }
  return best;
}

function relativeTime(iso: string): string {
  if (!iso) return '';
  const t = new Date(iso).getTime();
  if (!Number.isFinite(t)) return '';
  const diff = Math.max(0, Date.now() - t);
  const sec = Math.floor(diff / 1000);
  if (sec < 60) return `${sec}s ago`;
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m ago`;
  const hr = Math.floor(min / 60);
  if (hr < 24) return `${hr}h ago`;
  const day = Math.floor(hr / 24);
  return `${day}d ago`;
}

const localMatches = computed<Task[]>(() => {
  const q = query.value.trim();
  const all = taskStore.tasks;
  if (!q) return [];
  const scored: { task: Task; score: number }[] = [];
  for (const t of all) {
    const r = matchTask(t, q);
    if (r && r.matched) scored.push({ task: t, score: r.score });
  }
  scored.sort((a, b) => {
    if (b.score !== a.score) return b.score - a.score;
    return taskTitle(a.task).localeCompare(taskTitle(b.task));
  });
  return scored.map((s) => s.task);
});

const recentTasks = computed<Task[]>(() => {
  return [...taskStore.tasks]
    .sort((a, b) => {
      const at = new Date(a.updated_at || a.created_at || 0).getTime();
      const bt = new Date(b.updated_at || b.created_at || 0).getTime();
      return bt - at;
    })
    .slice(0, 10);
});

interface Section {
  title: string;
  tasks: Task[];
}

const sections = computed<Section[]>(() => {
  const q = query.value.trim();
  if (!q) {
    return recentTasks.value.length
      ? [{ title: 'Recent', tasks: recentTasks.value }]
      : [];
  }
  const out: Section[] = [];
  if (localMatches.value.length) {
    out.push({ title: 'Tasks', tasks: localMatches.value });
  }
  // Append remote results that aren't already shown locally.
  if (remoteResults.value.length) {
    const localIds = new Set(localMatches.value.map((t) => t.id));
    const extras = remoteResults.value.filter((t) => !localIds.has(t.id));
    if (extras.length) {
      out.push({ title: 'From archive', tasks: extras });
    }
  }
  return out;
});

const flatRows = computed<Task[]>(() => {
  const out: Task[] = [];
  for (const s of sections.value) for (const t of s.tasks) out.push(t);
  return out;
});

function clampActive() {
  const len = flatRows.value.length;
  if (!len) {
    activeIndex.value = 0;
    return;
  }
  if (activeIndex.value >= len) activeIndex.value = len - 1;
  if (activeIndex.value < 0) activeIndex.value = 0;
}

watch(flatRows, () => {
  activeIndex.value = 0;
});

watch(
  () => props.modelValue,
  (open) => {
    if (open) {
      query.value = '';
      remoteResults.value = [];
      activeIndex.value = 0;
      nextTick(() => inputRef.value?.focus());
    } else {
      // Clear pending server search when closing.
      if (serverTimer) {
        clearTimeout(serverTimer);
        serverTimer = null;
      }
      serverSeq++;
    }
  },
);

watch(query, (q) => {
  // Reset remote when query empties or changes to short.
  if (!q || q.trim().length < 2) {
    remoteResults.value = [];
    if (serverTimer) {
      clearTimeout(serverTimer);
      serverTimer = null;
    }
    return;
  }
  // Only trigger server-side search when local has no matches.
  if (localMatches.value.length > 0) {
    remoteResults.value = [];
    return;
  }
  if (serverTimer) clearTimeout(serverTimer);
  const trimmed = q.trim();
  const seq = ++serverSeq;
  serverTimer = setTimeout(async () => {
    try {
      const res = await api<{ tasks: Task[] }>(
        'GET',
        `/api/tasks/search?q=${encodeURIComponent(trimmed)}`,
      );
      if (seq !== serverSeq) return;
      remoteResults.value = Array.isArray(res?.tasks) ? res.tasks : [];
    } catch {
      if (seq !== serverSeq) return;
      remoteResults.value = [];
    }
  }, 200);
});

function close() {
  emit('update:modelValue', false);
}

function pick(task: Task) {
  emit('select', task.id);
  if (route.path !== '/') {
    router.push({ path: '/', query: { task: task.id } });
  } else if (route.query.task !== task.id) {
    router.replace({ path: '/', query: { ...route.query, task: task.id } });
  }
  close();
}

function indexFor(task: Task): number {
  return flatRows.value.indexOf(task);
}

function onKeydown(e: KeyboardEvent) {
  switch (e.key) {
    case 'ArrowDown': {
      e.preventDefault();
      const len = flatRows.value.length;
      if (!len) return;
      activeIndex.value = (activeIndex.value + 1) % len;
      break;
    }
    case 'ArrowUp': {
      e.preventDefault();
      const len = flatRows.value.length;
      if (!len) return;
      activeIndex.value = (activeIndex.value - 1 + len) % len;
      break;
    }
    case 'Enter': {
      e.preventDefault();
      clampActive();
      const row = flatRows.value[activeIndex.value];
      if (row) pick(row);
      break;
    }
    case 'Escape':
      e.preventDefault();
      close();
      break;
  }
}

function onOverlayClick(e: MouseEvent) {
  if (e.target === e.currentTarget) close();
}

function globalToggle(e: KeyboardEvent) {
  if ((e.key === 'k' || e.key === 'K') && (e.ctrlKey || e.metaKey)) {
    const tag = (document.activeElement?.tagName || '').toUpperCase();
    const editable =
      document.activeElement instanceof HTMLElement &&
      document.activeElement.isContentEditable;
    if (tag === 'INPUT' || tag === 'TEXTAREA' || editable) {
      // Allow toggle from anywhere when our own palette input is focused.
      if (!props.modelValue) return;
    }
    e.preventDefault();
    e.stopPropagation();
    emit('update:modelValue', !props.modelValue);
  }
}

onMounted(() => {
  window.addEventListener('keydown', globalToggle, true);
});
onUnmounted(() => {
  window.removeEventListener('keydown', globalToggle, true);
  if (serverTimer) clearTimeout(serverTimer);
});
</script>

<template>
  <Teleport to="body">
    <div
      v-if="modelValue"
      class="modal-overlay command-palette"
      @click="onOverlayClick"
      @keydown="onKeydown"
    >
      <div class="command-palette-panel" @click.stop>
        <div class="command-palette-header">
          <span class="command-palette-label">
            <strong>Command palette</strong>
          </span>
          <span class="command-palette-hints">&#8984;/ Ctrl+K</span>
        </div>
        <input
          ref="inputRef"
          v-model="query"
          type="text"
          class="command-palette-input"
          placeholder="Search title, prompt, or task id"
          autocomplete="off"
        />
        <div class="command-palette-results">
          <template v-if="sections.length">
            <section
              v-for="section in sections"
              :key="section.title"
              class="command-palette-section"
            >
              <div class="command-palette-section-title">{{ section.title }}</div>
              <button
                v-for="task in section.tasks"
                :key="task.id"
                type="button"
                class="command-palette-row command-palette-row-task"
                :class="{ active: indexFor(task) === activeIndex }"
                @click="pick(task)"
                @mouseenter="activeIndex = indexFor(task)"
              >
                <div class="command-palette-row-title">
                  {{ taskTitle(task) || '(untitled)' }}
                </div>
                <div class="command-palette-row-meta">
                  <span class="command-palette-task-id">{{ shortId(task) }}</span>
                  <span
                    v-if="task.status"
                    class="badge"
                    :class="`badge-${task.status}`"
                  >{{ task.status }}</span>
                  <span class="command-palette-row-hint-time">
                    {{ relativeTime(task.updated_at || task.created_at) }}
                  </span>
                </div>
                <div
                  v-if="!task.title && task.prompt"
                  class="command-palette-task-snippet"
                >{{ task.prompt.slice(0, 180) }}</div>
              </button>
            </section>
          </template>
          <div v-else class="command-palette-empty">
            {{ query.trim() ? 'No matches' : 'No tasks yet' }}
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>
