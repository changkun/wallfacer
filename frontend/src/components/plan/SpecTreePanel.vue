<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { specStatusTone } from '../../lib/specStatus';
import { storeToRefs } from 'pinia';
import { api } from '../../api/client';
import { useAgentStore } from '../../stores/agentSession';
import type { SpecNode, SpecIndexMeta, SpecProgress } from '../../stores/agentSession';
import { isNodeCheckable, nodeUnmetDeps, selectableRange } from './specTreeSelect';
import { useTaskStore } from '../../stores/tasks';
import { useUiStore } from '../../stores/ui';
import { useDialogStore } from '../../stores/dialog';
import AppSelect from '../AppSelect.vue';

// Folding the tree to a rail is owned by the parent (PlanPage) which controls
// the layout slot; we just ask to be collapsed.
const emit = defineEmits<{ collapse: [] }>();

const agentStore = useAgentStore();
const taskStore = useTaskStore();
const ui = useUiStore();
const dialog = useDialogStore();
const {
  tree, treeProgress, treeIndex, treeGroups, treeLoading,
  focusedSpecPath, focusedIsIndex, focusedTaskId, staleCandidates,
} = storeToRefs(agentStore);

const rescanning = ref(false);
async function onRescanStaleness() {
  rescanning.value = true;
  try {
    await agentStore.fetchStaleCandidates();
  } finally {
    rescanning.value = false;
  }
}

const staleCandidateCount = computed(() => Object.keys(staleCandidates.value).length);

async function onDismissAllStaleness() {
  const n = staleCandidateCount.value;
  if (n === 0) return;
  const ok = await dialog.confirm({
    title: 'Dismiss all stale candidates',
    message: `Mark ${n} flagged spec${n === 1 ? '' : 's'} as reviewed? This bumps each one's `
      + `updated date (status unchanged), asserting the designs still match the code.`,
    confirmLabel: 'Dismiss all',
  });
  if (!ok) return;
  rescanning.value = true;
  try {
    await agentStore.dismissAllStaleCandidates();
  } finally {
    rescanning.value = false;
  }
}

// ── Persisted UI state ─────────────────────────────────────────────
const STATUS_KEY = 'wallfacer-spec-filter';
const EXPANDED_KEY = 'wallfacer-spec-expanded';
const ARCHIVED_KEY = 'wallfacer-spec-show-archived';

function readSet(key: string): Set<string> {
  try {
    return new Set<string>(JSON.parse(localStorage.getItem(key) ?? '[]'));
  } catch {
    return new Set<string>();
  }
}

const expandedPaths = ref<Set<string>>(readSet(EXPANDED_KEY));
const statusFilter = ref<string>(localStorage.getItem(STATUS_KEY) ?? 'all');
const showArchived = ref<boolean>(localStorage.getItem(ARCHIVED_KEY) === 'true');
const textFilter = ref<string>('');
const selectedPaths = ref<Set<string>>(new Set());
const lastCheckedIndex = ref<number>(-1);

// ── Task Prompts virtual section ──────────────────────────────────

const TASK_PROMPTS_EXPANDED_KEY = 'wallfacer-task-prompts-expanded';
const TASK_PROMPTS_WAITING_KEY = 'wallfacer-task-prompts-waiting';

interface TaskPromptEntry {
  task_id: string;
  title: string;
  status: string;
  updated_at: string;
}

const taskPrompts = ref<TaskPromptEntry[]>([]);
const taskPromptsExpanded = ref<boolean>(
  localStorage.getItem(TASK_PROMPTS_EXPANDED_KEY) !== '0',
);
const taskPromptsIncludeWaiting = ref<boolean>(
  localStorage.getItem(TASK_PROMPTS_WAITING_KEY) === '1',
);

async function loadTaskPrompts() {
  const url =
    '/api/explorer/task-prompts' +
    (taskPromptsIncludeWaiting.value ? '?status=backlog,waiting' : '');
  try {
    const data = await api<TaskPromptEntry[]>('GET', url);
    taskPrompts.value = Array.isArray(data) ? data : [];
  } catch {
    taskPrompts.value = [];
  }
}

function toggleTaskPromptsExpanded() {
  taskPromptsExpanded.value = !taskPromptsExpanded.value;
  localStorage.setItem(
    TASK_PROMPTS_EXPANDED_KEY,
    taskPromptsExpanded.value ? '1' : '0',
  );
}

function toggleTaskPromptsWaiting(ev: Event) {
  ev.stopPropagation();
  taskPromptsIncludeWaiting.value = !taskPromptsIncludeWaiting.value;
  localStorage.setItem(
    TASK_PROMPTS_WAITING_KEY,
    taskPromptsIncludeWaiting.value ? '1' : '0',
  );
  void loadTaskPrompts();
}

async function openTaskPrompt(entry: TaskPromptEntry) {
  // Pull the freshest prompt from the in-memory tasks store if available.
  const cached = taskStore.tasks.find(t => t.id === entry.task_id);
  const prompt = cached?.prompt ?? entry.title;
  await agentStore.openPlanForTask(entry.task_id, entry.title, prompt);
}

function persistExpanded() {
  localStorage.setItem(EXPANDED_KEY, JSON.stringify([...expandedPaths.value]));
}


// ── Filtering ──────────────────────────────────────────────────────

function nodeMatches(node: SpecNode, byPath: Map<string, SpecNode>): boolean {
  const spec = node.spec;
  if (!spec) return false;
  if (spec.status === 'archived' && !showArchived.value) return false;

  let statusOk = true;
  if (statusFilter.value !== 'all') {
    statusOk =
      statusFilter.value === 'incomplete'
        ? spec.status !== 'complete' && spec.status !== 'archived'
        : spec.status === statusFilter.value;
  }

  let textOk = true;
  if (textFilter.value) {
    const q = textFilter.value.toLowerCase();
    textOk = (spec.title ?? '').toLowerCase().includes(q) || node.path.toLowerCase().includes(q);
  }

  const selfOk = statusOk && textOk;
  if (node.is_leaf) return selfOk;
  if (selfOk) return true;
  for (const childPath of node.children ?? []) {
    const child = byPath.get(childPath);
    if (child && nodeMatches(child, byPath)) return true;
  }
  return false;
}

interface RenderedNode {
  node: SpecNode;
  depth: number;
  hasChildren: boolean;
  expanded: boolean;
}

interface RenderedTrack {
  key: string; // expansion key (namespaced by folder so groups toggle independently)
  name: string;
  expanded: boolean;
  nodes: RenderedNode[];
}

// A workspace folder's spec subtree, rendered separately when the workspace
// spans multiple folders that each have specs/.
interface RenderedGroup {
  key: string;
  label: string;
  showHeader: boolean;
  index: SpecIndexMeta | null;
  progress: Record<string, SpecProgress>;
  tracks: RenderedTrack[];
}

// Shared path->node index over the whole tree. Reused by renderedTracks,
// unmetDeps and the shift-range selection sweep so the map is built once per
// tree change rather than per call.
const byPath = computed(() => {
  const m = new Map<string, SpecNode>();
  for (const n of tree.value) m.set(n.path, n);
  return m;
});

// buildTracks renders a set of root-and-descendant nodes into expandable tracks,
// using the supplied path index (so each folder resolves children within its own
// subtree). keyPrefix namespaces the track expansion keys so two folders that
// both have a `local/` track toggle independently.
function buildTracks(nodes: SpecNode[], byPathMap: Map<string, SpecNode>, keyPrefix: string): RenderedTrack[] {
  const trackOrder: string[] = [];
  const groups: Record<string, SpecNode[]> = {};
  for (const node of nodes) {
    if (node.depth !== 0) continue;
    // Loose top-level specs (no folder) carry an empty track; group them
    // under "other" so they still render rather than under a blank header.
    const track = node.spec?.track || 'other';
    if (!groups[track]) {
      groups[track] = [];
      trackOrder.push(track);
    }
    groups[track].push(node);
  }

  const out: RenderedTrack[] = [];
  for (const track of trackOrder) {
    const key = keyPrefix + '__track__' + track;
    const expanded = expandedPaths.value.has(key) || !!textFilter.value;
    const rnodes: RenderedNode[] = [];

    function walk(node: SpecNode, depth: number) {
      if (!nodeMatches(node, byPathMap)) return;
      const hasChildren = (node.children?.length ?? 0) > 0;
      const isExpanded = expandedPaths.value.has(node.path) || !!textFilter.value;
      rnodes.push({ node, depth, hasChildren, expanded: isExpanded });
      if (hasChildren && isExpanded) {
        for (const childPath of node.children ?? []) {
          const child = byPathMap.get(childPath);
          if (child) walk(child, depth + 1);
        }
      }
    }

    if (expanded) {
      for (const root of groups[track]) walk(root, 0);
    }

    out.push({ key, name: track, expanded, nodes: rnodes });
  }
  return out;
}

// renderedGroups splits the tree by workspace folder. The server sends one
// self-contained group per folder; when only one folder has specs there is a
// single group (no folder header). Falls back to the flat tree if an older
// payload arrives without groups.
const renderedGroups = computed<RenderedGroup[]>(() => {
  const source = treeGroups.value.length > 0
    ? treeGroups.value
    : (tree.value.length > 0
        ? [{ workspace: '', label: '', nodes: tree.value, progress: treeProgress.value, index: treeIndex.value }]
        : []);
  const multi = source.length > 1;
  return source.map((g) => {
    const bp = new Map<string, SpecNode>();
    for (const n of g.nodes) bp.set(n.path, n);
    return {
      key: g.workspace || '__single__',
      label: g.label,
      showHeader: multi,
      index: g.index,
      progress: g.progress,
      tracks: buildTracks(g.nodes, bp, multi ? g.workspace : ''),
    };
  });
});

// ── Interactions ───────────────────────────────────────────────────

function toggleTrack(key: string) {
  if (expandedPaths.value.has(key)) expandedPaths.value.delete(key);
  else expandedPaths.value.add(key);
  expandedPaths.value = new Set(expandedPaths.value);
  persistExpanded();
}

function toggleNode(path: string) {
  if (expandedPaths.value.has(path)) expandedPaths.value.delete(path);
  else expandedPaths.value.add(path);
  expandedPaths.value = new Set(expandedPaths.value);
  persistExpanded();
}

function selectNode(node: SpecNode) {
  agentStore.focusSpec(node.path);
}

function selectIndex() {
  if (treeIndex.value) agentStore.focusIndex();
}

function setStatusFilter(v: string) {
  statusFilter.value = v;
  localStorage.setItem(STATUS_KEY, v);
}

const statusFilterOptions = computed(() => [
  { value: 'all', label: 'All' },
  { value: 'incomplete', label: 'Incomplete' },
  { value: 'vague', label: 'Vague' },
  { value: 'drafted', label: 'Drafted' },
  { value: 'validated', label: 'Validated' },
  { value: 'testing', label: 'Testing' },
  { value: 'complete', label: 'Complete' },
  { value: 'stale', label: 'Stale' },
  { value: 'archived', label: 'Archived', disabled: !showArchived.value },
]);

function toggleShowArchived() {
  showArchived.value = !showArchived.value;
  localStorage.setItem(ARCHIVED_KEY, String(showArchived.value));
  if (showArchived.value) {
    // Force-collapse archived parents so they don't flood the view.
    for (const n of tree.value) {
      if (n.spec?.status === 'archived' && expandedPaths.value.has(n.path)) {
        expandedPaths.value.delete(n.path);
      }
    }
    expandedPaths.value = new Set(expandedPaths.value);
    persistExpanded();
  }
  if (!showArchived.value && statusFilter.value === 'archived') setStatusFilter('all');
}

// ── Multi-select dispatch ──────────────────────────────────────────

function unmetDeps(node: SpecNode): string[] {
  return nodeUnmetDeps(node, byPath.value);
}

function isCheckable(node: SpecNode): boolean {
  return isNodeCheckable(node);
}

const flatLeafIndex = computed(() => {
  const list: string[] = [];
  for (const g of renderedGroups.value) for (const t of g.tracks) for (const n of t.nodes) list.push(n.node.path);
  return list;
});

function onCheckboxChange(ev: Event, node: SpecNode) {
  const target = ev.target as HTMLInputElement;
  const idx = flatLeafIndex.value.indexOf(node.path);
  if ((ev as MouseEvent).shiftKey && lastCheckedIndex.value >= 0 && idx >= 0) {
    // Only sweep specs that are themselves checkable and unblocked, matching
    // the checkbox template gating; otherwise shift-range inflates the count
    // and triggers dispatch failures on non-validated/blocked specs.
    const range = selectableRange(
      flatLeafIndex.value, byPath.value, lastCheckedIndex.value, idx,
    );
    for (const path of range) {
      if (target.checked) selectedPaths.value.add(path);
      else selectedPaths.value.delete(path);
    }
  }
  if (target.checked) selectedPaths.value.add(node.path);
  else selectedPaths.value.delete(node.path);
  selectedPaths.value = new Set(selectedPaths.value);
  lastCheckedIndex.value = idx;
}

const dispatchPending = ref(false);

interface DispatchResp {
  dispatched?: { spec_path: string; task_id: string }[];
  errors?: { spec_path: string; error: string }[];
}

async function dispatchSelected() {
  const paths = [...selectedPaths.value];
  if (paths.length === 0) return;
  if (!(await dialog.confirm({
    title: 'Dispatch specs',
    message: `Dispatch ${paths.length} specs to the task board?`,
    confirmLabel: 'Dispatch',
  }))) return;
  dispatchPending.value = true;
  try {
    const resp = await api<DispatchResp>('POST', '/api/specs/transition', { action: 'dispatch', paths, run: false });
    selectedPaths.value = new Set();
    // Pulse the freshly-created cards when the board next renders them.
    ui.markDispatched((resp.dispatched ?? []).map((d) => d.task_id).filter(Boolean));
    if (resp.errors && resp.errors.length > 0) {
      const lines = resp.errors.map(e => `${e.spec_path}: ${e.error}`).join('\n');
      const dispatched = resp.dispatched?.length ?? 0;
      await dialog.alert(
        dispatched > 0
          ? `Dispatched ${dispatched}. ${resp.errors.length} failed:\n${lines}`
          : `Dispatch failed:\n${lines}`,
      );
    }
  } catch (e) {
    await dialog.alert('Dispatch failed: ' + (e instanceof Error ? e.message : String(e)));
  } finally {
    dispatchPending.value = false;
  }
}

// ── Free-form spec migration ───────────────────────────────────────
// Frontmatter-less files render as read-only "doc" nodes. Offer a
// dismissible nudge to adopt wallfacer frontmatter (one POST per file).
// Dismissal persists so we don't nag a user who is happy with render-only.
const MIGRATE_DISMISSED_KEY = 'wallfacer-spec-migrate-dismissed';
const migrateDismissed = ref<boolean>(localStorage.getItem(MIGRATE_DISMISSED_KEY) === 'true');
const migratePending = ref(false);

const docNodes = computed(() => tree.value.filter((n) => n.spec?.doc));

function dismissMigrate() {
  migrateDismissed.value = true;
  localStorage.setItem(MIGRATE_DISMISSED_KEY, 'true');
}

async function adoptDocNodes() {
  const paths = docNodes.value.map((n) => n.path);
  if (paths.length === 0) return;
  if (!(await dialog.confirm({
    title: 'Adopt spec frontmatter',
    message: `Add wallfacer frontmatter to ${paths.length} free-form spec${paths.length === 1 ? '' : 's'}? `
      + 'Each becomes a lifecycle-managed spec (status: drafted); the prose is preserved.',
    confirmLabel: 'Adopt',
  }))) return;
  migratePending.value = true;
  const errors: string[] = [];
  try {
    for (const path of paths) {
      try {
        await api('POST', '/api/specs/transition', { action: 'migrate', path });
      } catch (e) {
        errors.push(`${path}: ${e instanceof Error ? e.message : String(e)}`);
      }
    }
    await agentStore.fetchTree();
    if (errors.length > 0) {
      await dialog.alert(`Adopted ${paths.length - errors.length}. ${errors.length} failed:\n${errors.join('\n')}`);
    }
  } finally {
    migratePending.value = false;
  }
}

// ── Lifecycle ──────────────────────────────────────────────────────
// The spec-tree fetch and SSE subscription are owned by the parent
// PlanPage so they run regardless of which layout (chat-first vs
// three-pane) is currently mounted. This panel only refreshes the
// Task Prompts side-list which lives in its own DOM.

onMounted(() => {
  void loadTaskPrompts();
  void agentStore.fetchStaleCandidates();
});

// Keep the Task Prompts list fresh against the SSE-synced task store: reload
// whenever a task is added/removed OR changes status/title (a backlog task
// starting moves it out of the list). Watching only .length missed those
// transitions. Mirrors the legacy spec-explorer SSE subscription.
const taskPromptsSignal = computed(() =>
  taskStore.tasks.map((t) => `${t.id}:${t.status}:${t.title ? 1 : 0}`).join(','),
);
watch(taskPromptsSignal, () => { void loadTaskPrompts(); });

onUnmounted(() => {
  selectedPaths.value = new Set();
});
</script>

<template>
  <aside class="spec-tree-panel">
    <div class="stp-toolbar">
      <div class="stp-head">
        <AppSelect
          :model-value="statusFilter"
          :options="statusFilterOptions"
          class="stp-status"
          aria-label="Filter by status"
          @update:model-value="setStatusFilter"
        />
        <button
          type="button"
          class="icon-btn sm stp-collapse"
          title="Collapse spec tree"
          aria-label="Collapse spec tree"
          @click="emit('collapse')"
        >
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <polyline points="15 18 9 12 15 6"></polyline>
          </svg>
        </button>
      </div>
      <label class="stp-archived-toggle">
        <input type="checkbox" :checked="showArchived" @change="toggleShowArchived" />
        Show archived
      </label>
      <button
        type="button"
        class="btn sm ghost stp-rescan"
        :disabled="rescanning"
        title="Rescan completed specs for code drift in their affects files"
        @click="onRescanStaleness"
      >{{ rescanning ? 'Rescanning…' : 'Rescan staleness' }}</button>
      <button
        v-if="staleCandidateCount > 0"
        type="button"
        class="btn sm ghost stp-rescan"
        :disabled="rescanning"
        title="Mark all flagged specs reviewed (bumps their updated date; status unchanged)"
        @click="onDismissAllStaleness"
      >Dismiss all ({{ staleCandidateCount }})</button>
    </div>

    <div v-if="docNodes.length > 0 && !migrateDismissed" class="stp-migrate-banner">
      <span class="stp-migrate-text">
        {{ docNodes.length }} spec{{ docNodes.length === 1 ? '' : 's' }}
        {{ docNodes.length === 1 ? 'has' : 'have' }} no frontmatter and aren't lifecycle-managed.
      </span>
      <div class="stp-migrate-actions">
        <button
          type="button"
          class="btn sm stp-migrate-adopt"
          :disabled="migratePending"
          @click="adoptDocNodes"
        >{{ migratePending ? 'Adopting…' : 'Adopt frontmatter' }}</button>
        <button
          type="button"
          class="icon-btn sm stp-migrate-dismiss"
          title="Dismiss"
          aria-label="Dismiss"
          @click="dismissMigrate"
        >✕</button>
      </div>
    </div>

    <div class="stp-body">
      <div class="stp-task-prompts">
        <div
          class="stp-task-prompts-header"
          role="button"
          tabindex="0"
          :aria-expanded="taskPromptsExpanded"
          @click="toggleTaskPromptsExpanded"
          @keydown.enter.prevent="toggleTaskPromptsExpanded"
          @keydown.space.prevent="toggleTaskPromptsExpanded"
        >
          <span class="stp-chev" :class="{ open: taskPromptsExpanded }" />
          <span class="eyebrow stp-task-prompts-label">Task prompts</span>
          <button
            type="button"
            class="stp-task-prompts-waiting"
            :class="{ active: taskPromptsIncludeWaiting }"
            :title="taskPromptsIncludeWaiting ? 'Hide waiting tasks' : 'Show waiting tasks'"
            :aria-pressed="taskPromptsIncludeWaiting"
            @click="toggleTaskPromptsWaiting"
          >{{ taskPromptsIncludeWaiting ? 'W' : 'w' }}</button>
        </div>
        <template v-if="taskPromptsExpanded">
          <div v-if="taskPrompts.length === 0" class="stp-task-prompts-empty">
            No tasks
          </div>
          <button
            v-for="t in taskPrompts"
            :key="t.task_id"
            type="button"
            class="stp-task-prompt-item"
            :class="{ 'stp-task-prompt-item--focused': focusedTaskId === t.task_id }"
            :title="t.title"
            @click="openTaskPrompt(t)"
          >
            <span class="stp-task-prompt-status" :class="'stp-task-prompt-status--' + t.status" />
            <span class="stp-task-prompt-title">{{ t.title }}</span>
          </button>
        </template>
      </div>

      <div v-if="treeLoading" class="stp-empty">Loading specs…</div>
      <template v-else>
        <div
          v-if="treeIndex"
          class="stp-pinned"
          :class="{ 'stp-pinned--focused': focusedIsIndex }"
          role="button"
          tabindex="0"
          @click="selectIndex"
          @keydown.enter.prevent="selectIndex"
          @keydown.space.prevent="selectIndex"
        >
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><polyline points="14 2 14 8 20 8"></polyline></svg> Roadmap
        </div>

        <template v-for="group in renderedGroups" :key="group.key">
          <div v-if="group.showHeader" class="stp-group-header" :title="group.key">
{{ group.label }}
          </div>
          <div v-for="track in group.tracks" :key="track.key" class="stp-track">
            <div class="stp-track-header" @click="toggleTrack(track.key)">
              <span class="stp-chev" :class="{ open: track.expanded }" />
              <span class="eyebrow stp-track-name">{{ track.name }}</span>
            </div>
            <template v-if="track.expanded">
              <div
                v-for="rn in track.nodes"
                :key="rn.node.path"
                class="stp-node"
                :class="{
                  'stp-node--leaf': rn.node.is_leaf,
                  'stp-node--archived': rn.node.spec?.status === 'archived',
                  'stp-node--doc': rn.node.spec?.doc,
                  'stp-node--focused': focusedSpecPath === rn.node.path && !focusedIsIndex,
                }"
                :style="{ paddingLeft: 8 + rn.depth * 14 + 'px' }"
                @click="selectNode(rn.node)"
              >
                <span
                  v-if="rn.hasChildren"
                  class="stp-chev"
                  :class="{ open: rn.expanded }"
                  @click.stop="toggleNode(rn.node.path)"
                />
                <span v-else class="stp-chev-spacer" />
                <input
                  v-if="isCheckable(rn.node)"
                  type="checkbox"
                  class="stp-checkbox"
                  :checked="selectedPaths.has(rn.node.path)"
                  :disabled="unmetDeps(rn.node).length > 0"
                  :title="unmetDeps(rn.node).length > 0 ? 'Blocked by: ' + unmetDeps(rn.node).join(', ') : ''"
                  @click.stop
                  @change="onCheckboxChange($event, rn.node)"
                />
                <span class="stp-dot" :class="'stp-dot--' + specStatusTone(rn.node.spec?.status, !!rn.node.spec?.doc)" :title="rn.node.spec?.doc ? 'no frontmatter' : (rn.node.spec?.status ?? '')" />
                <span class="stp-title">{{ rn.node.spec?.title || rn.node.path }}</span>
                <span
                  v-if="staleCandidates[rn.node.path]"
                  class="stp-stale-candidate"
                  :title="staleCandidates[rn.node.path].reason"
                >⚠ stale candidate</span>
                <span
                  v-if="!rn.node.is_leaf && group.progress[rn.node.path]"
                  class="stp-progress"
                >{{ group.progress[rn.node.path].Complete }}/{{ group.progress[rn.node.path].Total }}</span>
              </div>
            </template>
          </div>
        </template>

        <div v-if="renderedGroups.every(g => g.tracks.length === 0)" class="stp-empty">No specs match the filter.</div>
      </template>
    </div>

    <div v-if="selectedPaths.size > 0" class="stp-dispatch-bar">
      <button
        type="button"
        class="btn block stp-dispatch-btn"
        :disabled="dispatchPending"
        @click="dispatchSelected"
      >
        {{ dispatchPending ? 'Dispatching…' : `Dispatch Selected (${selectedPaths.size})` }}
      </button>
    </div>
  </aside>
</template>

<style scoped>
/* The spec tree: a sunk column of nav rows. A row is the rail's row (34px,
   radius 12), the focused one sits on the card surface, and a 5px ramp dot
   carries the lifecycle status. See specs/shared/console-redesign/plan.md. */
.spec-tree-panel {
  width: var(--stp-width, 280px);
  min-width: 200px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--rule);
  background: var(--bg-sunk);
  overflow: hidden;
}
.stp-toolbar {
  padding: 10px 8px 8px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  border-bottom: 1px solid var(--rule);
}
.stp-head {
  display: flex;
  align-items: center;
  gap: 6px;
}
.stp-head .stp-status {
  flex: 1;
  min-width: 0;
}
.stp-head .stp-collapse {
  flex: none;
}
.stp-status :deep(.app-select__trigger) {
  min-height: 32px;
  font-size: var(--fs-base);
  padding: 4px 10px;
  border: 1px solid var(--rule);
  border-radius: var(--r-md);
  background: var(--bg-card);
  color: var(--ink);
  cursor: pointer;
}
.stp-status :deep(.app-select__trigger:focus-visible) {
  outline: none;
  border-color: var(--accent-line);
  box-shadow: 0 0 0 3px var(--accent-ring);
}
.stp-archived-toggle {
  font-size: var(--fs-10);
  color: var(--ink-3);
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 0 2px;
  cursor: pointer;
}
.stp-archived-toggle input {
  margin: 0;
  accent-color: var(--accent);
}
.stp-rescan {
  align-self: flex-start;
}
.stp-stale-candidate {
  font: 600 var(--fs-9) / 1.5 var(--font-mono);
  letter-spacing: 0.04em;
  color: var(--warn);
  background: var(--tint-amber);
  border-radius: var(--r-pill);
  padding: 0 7px;
  white-space: nowrap;
}
.stp-migrate-banner {
  display: flex;
  align-items: center;
  gap: 8px;
  margin: 8px;
  padding: 8px 10px;
  font-size: var(--fs-10);
  color: var(--ink-2);
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-lg);
}
.stp-migrate-text {
  flex: 1;
  line-height: 1.4;
}
.stp-migrate-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}
.stp-body {
  flex: 1;
  overflow-y: auto;
  padding: 6px 6px 12px;
  font-size: var(--fs-base);
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.stp-empty {
  padding: 16px 12px;
  color: var(--ink-4);
  text-align: center;
  font-size: 12px;
}
.stp-pinned,
.stp-node,
.stp-task-prompt-item {
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 34px;
  padding: 0 10px;
  border: none;
  border-radius: var(--r-row);
  background: transparent;
  color: var(--ink-2);
  font: inherit;
  font-size: var(--fs-base);
  font-weight: 500;
  text-align: left;
  cursor: pointer;
  line-height: 1.35;
  transition: background var(--dur-hover), color var(--dur-hover);
}
.stp-pinned:hover,
.stp-node:hover,
.stp-task-prompt-item:hover {
  background: color-mix(in srgb, var(--ink) 5%, transparent);
  color: var(--ink);
}
.stp-pinned--focused,
.stp-node--focused,
.stp-task-prompt-item--focused {
  background: var(--bg-card);
  color: var(--ink);
  font-weight: 600;
  box-shadow: var(--sh-card);
}
.stp-pinned {
  margin-bottom: 4px;
}
.stp-pinned svg {
  color: var(--ink-3);
  flex: none;
}
.stp-group-header {
  padding: 12px 10px 4px;
  font: 600 var(--fs-9) / 1 var(--font-mono);
  letter-spacing: var(--tracking-label);
  text-transform: uppercase;
  color: var(--ink-2);
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 6px;
  border-top: 1px solid var(--rule);
}
.stp-track-header {
  display: flex;
  align-items: center;
  gap: 4px;
  min-height: 28px;
  padding: 0 6px;
  border-radius: var(--r-sm);
  cursor: pointer;
  margin-top: 6px;
}
.stp-track-header:hover {
  background: color-mix(in srgb, var(--ink) 5%, transparent);
}
.stp-track-name {
  flex: 1;
  letter-spacing: 0.12em;
}
.stp-node--archived {
  opacity: 0.55;
}
.stp-node--doc .stp-title {
  font-style: italic;
  color: var(--ink-2);
}
.stp-chev,
.stp-chev-spacer {
  width: 16px;
  height: 16px;
  flex-shrink: 0;
}
/* Crisp chevron drawn via an SVG mask in currentColor. */
.stp-chev {
  background-color: var(--ink-3);
  -webkit-mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'%3E%3Cpath d='M6 4l4 4-4 4' fill='none' stroke='%23000' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E") center / 11px 11px no-repeat;
  mask: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'%3E%3Cpath d='M6 4l4 4-4 4' fill='none' stroke='%23000' stroke-width='2' stroke-linecap='round' stroke-linejoin='round'/%3E%3C/svg%3E") center / 11px 11px no-repeat;
  cursor: pointer;
  transition: transform 0.15s var(--ease-fluid), background-color var(--dur-hover);
}
.stp-chev:hover {
  background-color: var(--ink);
}
.stp-chev.open {
  transform: rotate(90deg);
  background-color: var(--ink-2);
}
.stp-checkbox {
  margin-right: 2px;
  flex-shrink: 0;
  accent-color: var(--accent);
}
/* The status dot: one ramp color per lifecycle status. */
.stp-dot {
  flex: none;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--ink-4);
}
.stp-dot--neutral { background: var(--ink-3); }
.stp-dot--run { background: var(--run); }
.stp-dot--warn { background: var(--warn); }
.stp-dot--ok { background: var(--ok); }
.stp-dot--err { background: var(--err); }
.stp-dot--muted { background: transparent; box-shadow: inset 0 0 0 1.5px var(--ink-4); }
.stp-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.stp-progress {
  flex-shrink: 0;
  font: 500 var(--fs-10) / 1 var(--font-mono);
  color: var(--ink-3);
  font-variant-numeric: tabular-nums;
}
.stp-task-prompts {
  border-bottom: 1px solid var(--rule);
  padding-bottom: 6px;
  margin-bottom: 4px;
}
.stp-task-prompts-header {
  display: flex;
  align-items: center;
  gap: 6px;
  min-height: 28px;
  padding: 0 6px;
  border-radius: var(--r-sm);
  cursor: pointer;
  user-select: none;
}
.stp-task-prompts-header:hover {
  background: color-mix(in srgb, var(--ink) 5%, transparent);
}
.stp-task-prompts-label {
  flex: 1;
  letter-spacing: 0.12em;
}
.stp-task-prompts-waiting {
  background: transparent;
  border: 1px solid var(--rule-2);
  border-radius: var(--r-xs);
  font: 700 var(--fs-9) / 1 var(--font-mono);
  width: 18px;
  height: 16px;
  cursor: pointer;
  color: var(--ink-3);
  padding: 0;
}
.stp-task-prompts-waiting:hover { color: var(--ink); }
.stp-task-prompts-waiting.active {
  background: var(--accent);
  color: var(--accent-fg);
  border-color: var(--accent);
}
.stp-task-prompts-empty {
  padding: 6px 14px;
  color: var(--ink-4);
  font-size: var(--fs-10);
  font-style: italic;
}
.stp-task-prompt-item {
  width: 100%;
  padding-left: 26px;
}
.stp-task-prompt-status {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--ink-4);
  flex-shrink: 0;
}
.stp-task-prompt-status--backlog { background: var(--col-backlog); }
.stp-task-prompt-status--waiting { background: var(--warn); }
.stp-task-prompt-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.stp-dispatch-bar {
  padding: 8px;
  border-top: 1px solid var(--rule);
}
</style>
