<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted, onUnmounted } from 'vue';
import { useRouter, useRoute } from 'vue-router';
import { api } from '../api/client';
import { useTaskStore } from '../stores/tasks';
import { usePlanningStore, type SpecNode } from '../stores/planning';
import { useUiStore } from '../stores/ui';
import type { Task } from '../api/types';
import { commandPaletteActionsFor, CARD_ACTION_DEFS, type CardAction } from '../lib/cardActions';
import { docIndex } from '../data/docs';
import { rankDocs } from '../lib/docSearch';

const props = defineProps<{ modelValue: boolean }>();
const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  select: [taskId: string];
}>();

const router = useRouter();
const route = useRoute();
const taskStore = useTaskStore();
const planning = usePlanningStore();
const ui = useUiStore();

const query = ref('');
const activeIndex = ref(0);
const inputRef = ref<HTMLInputElement | null>(null);
// Server search rows embed the full Task plus a matched_field + HTML snippet
// (store.TaskSearchResult). The endpoint returns a bare array, not {tasks}.
type SearchTask = Task & { matched_field?: string; snippet?: string };
const remoteResults = ref<SearchTask[]>([]);

// Server-side docs content search (GET /api/docs-search). Unlike the static
// docIndex title/slug ranking, this matches the markdown body so the palette
// can find docs by content. Rows carry a context snippet.
interface DocRow {
  slug: string;
  title: string;
  snippet?: string;
}
const docContentResults = ref<DocRow[]>([]);
let docSeq = 0;
let docTimer: ReturnType<typeof setTimeout> | null = null;

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
  tasks: SearchTask[];
}

const sections = computed<Section[]>(() => {
  const q = query.value.trim();
  if (!q) {
    return recentTasks.value.length
      ? [{ title: 'Tasks', tasks: recentTasks.value }]
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

// Flat list of every keyboard-navigable row. Per-task actions interleave
// directly under their parent task so ArrowDown walks Start / Resume /
// Done / Retry without skipping. Doc rows live at the bottom.
type FlatRow =
  | { kind: 'task'; task: Task }
  | { kind: 'action'; task: Task; action: ReturnType<typeof taskActions>[number] }
  | { kind: 'jump'; task: Task; tab: string; label: string }
  | { kind: 'spec'; node: SpecNode }
  | { kind: 'doc'; slug: string };

// Tab-switch jumps: open a task's detail modal directly on a given tab
// (mirrors ui/js/command-palette.js's open-task-{changes,spans,timeline}).
function tabJumps(task: Task): { tab: string; label: string }[] {
  if (task.status === 'backlog') return [];
  const jumps = [{ tab: 'changes', label: 'Open changes' }];
  if (task.turns > 0) {
    jumps.push({ tab: 'verification', label: 'Open verification' });
    jumps.push({ tab: 'timeline', label: 'Open timeline' });
  }
  return jumps;
}

// docMatches must be declared BEFORE flatRows references it — the
// `<script setup>` compiler keeps the top-level order, so a forward
// reference produces a TDZ ReferenceError on first eval.
const docMatches = computed<DocRow[]>(() => {
  const q = query.value.trim();
  // On empty query, dump all docs (matches OLD's open-state Docs group).
  if (!q) return docIndex.slice(0, 20);
  // Once the server content search returns, prefer it — it matches the doc
  // body, not just the title/slug, and carries snippets.
  if (docContentResults.value.length) return docContentResults.value;
  // Instant fallback before the debounced server call resolves:
  // title-prefix > title-substring > slug (see lib/docSearch).
  return rankDocs(docIndex, q, 6);
});

// Spec rows for the Plan section: fuzzy match on spec title and path
// (mirrors ui/js/command-palette.js's spec rows).
const specMatches = computed<SpecNode[]>(() => {
  const q = query.value.trim();
  // On empty query, dump the spec tree (matches OLD's open-state Plan group).
  if (!q) return planning.tree.filter((n) => n.spec).slice(0, 20);
  const scored: { node: SpecNode; score: number }[] = [];
  for (const n of planning.tree) {
    const title = n.spec?.title || n.path;
    const rt = fuzzyMatch(title, q);
    const rp = fuzzyMatch(n.path, q);
    const best = Math.max(rt.matched ? rt.score : -1, rp.matched ? rp.score : -1);
    if (best > -1) scored.push({ node: n, score: best });
  }
  scored.sort((a, b) =>
    b.score - a.score || (a.node.spec?.title || a.node.path).localeCompare(b.node.spec?.title || b.node.path),
  );
  return scored.slice(0, 6).map((s) => s.node);
});

const flatRows = computed<FlatRow[]>(() => {
  const out: FlatRow[] = [];
  for (const s of sections.value) {
    for (const t of s.tasks) {
      out.push({ kind: 'task', task: t });
      for (const a of taskActions(t)) out.push({ kind: 'action', task: t, action: a });
      for (const j of tabJumps(t)) out.push({ kind: 'jump', task: t, tab: j.tab, label: j.label });
    }
  }
  for (const n of specMatches.value) out.push({ kind: 'spec', node: n });
  for (const d of docMatches.value) out.push({ kind: 'doc', slug: d.slug });
  return out;
});

function taskRowIndex(task: Task): number {
  return flatRows.value.findIndex((r) => r.kind === 'task' && r.task.id === task.id);
}
function actionRowIndex(task: Task, actionId: CardAction): number {
  return flatRows.value.findIndex((r) => r.kind === 'action' && r.task.id === task.id && r.action.id === actionId);
}
function jumpRowIndex(task: Task, tab: string): number {
  return flatRows.value.findIndex((r) => r.kind === 'jump' && r.task.id === task.id && r.tab === tab);
}
function specRowIndex(path: string): number {
  return flatRows.value.findIndex((r) => r.kind === 'spec' && r.node.path === path);
}
function docRowIndex(slug: string): number {
  return flatRows.value.findIndex((r) => r.kind === 'doc' && r.slug === slug);
}

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
      query.value = ui.paletteSeed || '';
      ui.paletteSeed = '';
      remoteResults.value = [];
      activeIndex.value = 0;
      // Load the spec tree once so the Plan section can match (best-effort).
      if (!planning.tree.length) void planning.fetchTree();
      nextTick(() => inputRef.value?.focus());
    } else {
      // Clear pending server search when closing.
      if (serverTimer) {
        clearTimeout(serverTimer);
        serverTimer = null;
      }
      serverSeq++;
      if (docTimer) {
        clearTimeout(docTimer);
        docTimer = null;
      }
      docSeq++;
      docContentResults.value = [];
    }
  },
);

watch(query, (q) => {
  // Docs content search runs on every query (independent of task matches) so
  // the palette can surface docs by body text, debounced like task search.
  scheduleDocSearch(q);
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
      const res = await api<SearchTask[] | { tasks: SearchTask[] }>(
        'GET',
        `/api/tasks/search?q=${encodeURIComponent(trimmed)}`,
      );
      if (seq !== serverSeq) return;
      // The endpoint returns a bare array; tolerate a {tasks} wrapper too.
      remoteResults.value = Array.isArray(res) ? res : (res?.tasks ?? []);
    } catch {
      if (seq !== serverSeq) return;
      remoteResults.value = [];
    }
  }, 200);
});

function scheduleDocSearch(q: string) {
  const trimmed = (q || '').trim();
  if (docTimer) clearTimeout(docTimer);
  if (trimmed.length < 2) {
    docContentResults.value = [];
    return;
  }
  const seq = ++docSeq;
  docTimer = setTimeout(async () => {
    try {
      const res = await api<DocRow[]>('GET', `/api/docs-search?q=${encodeURIComponent(trimmed)}`);
      if (seq !== docSeq) return;
      docContentResults.value = Array.isArray(res) ? res : [];
    } catch {
      if (seq !== docSeq) return;
      docContentResults.value = [];
    }
  }, 200);
}

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

function pickJump(task: Task, tab: string) {
  emit('select', task.id);
  router.push({ path: '/', query: { task: task.id, tab } });
  close();
}

function pickSpec(path: string) {
  router.push({ path: '/plan', query: { spec: path } });
  close();
}

function pickDoc(slug: string) {
  router.push({ path: `/docs/${slug}` });
  close();
}

function taskActions(task: Task) {
  return commandPaletteActionsFor(task).map((id) => CARD_ACTION_DEFS[id]);
}

async function runTaskAction(action: CardAction, task: Task) {
  const id = task.id;
  switch (action) {
    case 'start': await api('PATCH', `/api/tasks/${id}`, { status: 'in_progress' }); break;
    case 'retry': await api('PATCH', `/api/tasks/${id}`, { status: 'backlog' }); break;
    case 'done': await api('POST', `/api/tasks/${id}/done`); break;
    case 'resume': await api('POST', `/api/tasks/${id}/resume`); break;
    case 'test': await api('POST', `/api/tasks/${id}/test`); break;
    case 'sync': await api('POST', '/api/git/sync', { task_id: id }); break;
  }
  close();
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
      if (!row) break;
      if (row.kind === 'task') pick(row.task);
      else if (row.kind === 'action') void runTaskAction(row.action.id, row.task);
      else if (row.kind === 'jump') pickJump(row.task, row.tab);
      else if (row.kind === 'spec') pickSpec(row.node.path);
      else if (row.kind === 'doc') pickDoc(row.slug);
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
          <span class="command-palette-hints">{{ flatRows.length ? '↑↓ navigate • Enter run • Esc close' : 'Esc close' }}</span>
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
              <div
                v-for="task in section.tasks"
                :key="task.id"
                role="button"
                tabindex="0"
                class="command-palette-row command-palette-row-task"
                :class="{ active: taskRowIndex(task) === activeIndex }"
                @click="pick(task)"
                @keydown.enter="pick(task)"
                @mouseenter="activeIndex = taskRowIndex(task)"
              >
                <div class="command-palette-row-title">
                  {{ taskTitle(task) || '(untitled)' }}
                </div>
                <div class="command-palette-row-meta">
                  <span class="command-palette-task-id">{{ shortId(task) }}</span>
                  <span
                    v-if="task.matched_field"
                    class="badge command-palette-match-field"
                    :title="`Matched in ${task.matched_field}`"
                  >{{ task.matched_field }}</span>
                  <span
                    v-if="task.status"
                    class="badge"
                    :class="`badge-${task.status}`"
                  >{{ task.status }}</span>
                  <span class="command-palette-row-hint-time">
                    {{ relativeTime(task.updated_at || task.created_at) }}
                  </span>
                  <span class="command-palette-row-actions" @click.stop>
                    <button
                      v-for="a in taskActions(task)"
                      :key="a.id"
                      type="button"
                      class="command-palette-action-btn"
                      :class="[a.cls, { active: actionRowIndex(task, a.id) === activeIndex }]"
                      :title="a.title"
                      @click="runTaskAction(a.id, task)"
                      @mouseenter="activeIndex = actionRowIndex(task, a.id)"
                    >{{ a.icon }} {{ a.label }}</button>
                    <button
                      v-for="j in tabJumps(task)"
                      :key="j.tab"
                      type="button"
                      class="command-palette-action-btn"
                      :class="{ active: jumpRowIndex(task, j.tab) === activeIndex }"
                      :title="j.label"
                      @click="pickJump(task, j.tab)"
                      @mouseenter="activeIndex = jumpRowIndex(task, j.tab)"
                    >{{ j.label }}</button>
                  </span>
                </div>
                <!-- Server search rows carry a pre-escaped HTML snippet with
                     <mark> highlights; local rows fall back to a prompt preview. -->
                <!-- eslint-disable-next-line vue/no-v-html — snippet is server-escaped -->
                <div v-if="task.snippet" class="command-palette-task-snippet" v-html="task.snippet" />
                <div
                  v-else-if="task.prompt"
                  class="command-palette-task-snippet"
                >{{ task.prompt.slice(0, 180) }}</div>
              </div>
            </section>
          </template>
          <section v-if="specMatches.length || !query.trim()" class="command-palette-section">
            <div class="command-palette-section-title">Plan</div>
            <div v-if="!specMatches.length" class="command-palette-empty">No entries</div>
            <div
              v-for="n in specMatches"
              :key="n.path"
              role="button"
              tabindex="0"
              class="command-palette-row command-palette-row-task"
              :class="{ active: specRowIndex(n.path) === activeIndex }"
              @click="pickSpec(n.path)"
              @keydown.enter="pickSpec(n.path)"
              @mouseenter="activeIndex = specRowIndex(n.path)"
            >
              <div class="command-palette-row-title">{{ n.spec?.title || n.path }}</div>
              <div class="command-palette-row-meta">
                <span class="command-palette-task-id">{{ n.path }}</span>
                <span v-if="n.spec?.status" class="badge">{{ n.spec.status }}</span>
              </div>
            </div>
          </section>
          <section v-if="docMatches.length" class="command-palette-section">
            <div class="command-palette-section-title">Docs</div>
            <div
              v-for="d in docMatches"
              :key="d.slug"
              role="button"
              tabindex="0"
              class="command-palette-row command-palette-row-task"
              :class="{ active: docRowIndex(d.slug) === activeIndex }"
              @click="pickDoc(d.slug)"
              @keydown.enter="pickDoc(d.slug)"
              @mouseenter="activeIndex = docRowIndex(d.slug)"
            >
              <div class="command-palette-row-title">{{ d.title }}</div>
              <div v-if="d.snippet" class="command-palette-doc-snippet">{{ d.snippet }}</div>
              <div class="command-palette-row-meta">
                <span class="command-palette-task-id">{{ d.slug }}</span>
              </div>
            </div>
          </section>
          <div v-if="!sections.length && !specMatches.length && !docMatches.length" class="command-palette-empty">
            {{ query.trim() ? 'No matches' : 'No tasks yet' }}
          </div>
        </div>
      </div>
    </div>
  </Teleport>
</template>
