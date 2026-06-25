// Planning store — central state for the Plan view (spec tree, focused
// spec, planning chat threads, streaming state). The Vue replacement for
// the globals scattered across ui/js/spec-mode.js, ui/js/spec-explorer.js,
// and ui/js/planning-chat.js.

import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import { api } from '../api/client';
import { useToastStore } from './toast';

export interface SpecMeta {
  title?: string;
  status?: string;
  track?: string;
  depends_on?: string[];
  affects?: string[];
  effort?: string;
  created?: string;
  updated?: string;
  author?: string;
  dispatched_task_id?: string | null;
  // Drift-pipeline markers, present only while a spec is in testing.
  implementation_commit?: string | null;
  testing_pending?: string | null;
  // doc marks a free-form, frontmatter-less file surfaced as a render-only
  // node: no status, no lifecycle. The backend sets it; migrating the file
  // (adopting frontmatter) turns it into a normal managed spec.
  doc?: boolean;
}

export interface SpecNode {
  path: string;
  spec: SpecMeta;
  children: string[];
  is_leaf: boolean;
  depth: number;
}

export interface SpecProgress {
  Complete: number;
  Total: number;
}

export interface SpecIndexMeta {
  path: string;
  workspace: string;
  title?: string;
  modified?: string;
}

export interface SpecTreeData {
  nodes: SpecNode[];
  index: SpecIndexMeta | null;
  progress: Record<string, SpecProgress>;
}

export interface PlanningThread {
  id: string;
  name: string;
  archived: boolean;
  mode: 'spec' | 'task' | '';
  task_id: string;
  unread: boolean;
  scrollTop: number;
  queue: { id: number; text: string }[];
  enqueuedAt: number;
  lastViewedAt: number;
  // Server timestamps (epoch ms). `updated` tracks last activity (touched on
  // every message), so the session list buckets and sorts by it.
  created: number;
  updated: number;
}

export interface PlanningMessage {
  role: 'user' | 'assistant' | 'system';
  content: string;
  timestamp?: string;
  raw_output?: string;
  plan_round?: number;
}

// parseTime turns an RFC3339 server timestamp into epoch ms, or 0 if absent or
// unparseable (so callers can fall back to a previous value).
function parseTime(s: string | undefined): number {
  if (!s) return 0;
  const ms = Date.parse(s);
  return Number.isNaN(ms) ? 0 : ms;
}

export const usePlanningStore = defineStore('planning', () => {
  const tree = ref<SpecNode[]>([]);
  const treeProgress = ref<Record<string, SpecProgress>>({});
  const treeIndex = ref<SpecIndexMeta | null>(null);
  const treeLoading = ref(true);

  // Advisory stale-candidate scan results, keyed by spec path. Populated on
  // demand (plan mount + manual rescan), not on every tree poll — the scan
  // runs git log per complete spec.
  const staleCandidates = ref<Record<string, { files: string[]; reason: string }>>({});

  const focusedSpecPath = ref<string>('');
  const focusedIsIndex = ref(false);
  const focusedTaskId = ref<string>('');
  const focusedTaskTitle = ref<string>('');
  const focusedTaskPrompt = ref<string>('');

  const threads = ref<Record<string, PlanningThread>>({});
  const threadOrder = ref<string[]>([]);
  const archivedThreads = ref<PlanningThread[]>([]);
  const activeThreadId = ref<string>('');

  const streaming = ref(false);
  const streamingThreadId = ref<string>('');
  // Thread the server reports as having an in-flight agent turn. Refreshed by a
  // light poll so the session list can show a spinner on a session that is
  // working in the background, even while the user views another one.
  const busyThreadId = ref<string>('');

  const sortedNodes = computed(() =>
    [...tree.value].sort((a, b) => a.path.localeCompare(b.path)),
  );

  const nodesByPath = computed(() => {
    const m = new Map<string, SpecNode>();
    for (const n of tree.value) m.set(n.path, n);
    return m;
  });

  const focusedNode = computed(() =>
    focusedSpecPath.value ? nodesByPath.value.get(focusedSpecPath.value) ?? null : null,
  );

  // First-spec bootstrap choreography: when a previously empty spec tree
  // gains its first node, auto-focus that node and surface a toast so the
  // workspace transition reads as a single fluid event.
  // Idempotent per session — reconnect-induced re-snapshots are ignored.
  let bootstrapFired = false;
  // The initial load (first applyTree) only establishes the baseline —
  // whether the workspace already has specs or none. It must never be
  // mistaken for the user creating their very first spec, or every fresh
  // page load on a populated workspace would fire the bootstrap toast.
  let initialLoadDone = false;

  function maybeFireBootstrap(nextNodes: SpecNode[]) {
    if (!initialLoadDone) {
      initialLoadDone = true;
      return;
    }
    if (bootstrapFired) return;
    if (tree.value.length > 0 || nextNodes.length === 0) return;
    bootstrapFired = true;
    const first = [...nextNodes].sort((a, b) => a.path.localeCompare(b.path))[0];
    if (!first?.path) return;
    setTimeout(() => focusSpec(first.path), 130);
    setTimeout(() => {
      try {
        useToastStore().push(
          `Your first spec was created at ${first.path}. Rename or move it anytime.`,
          { kind: 'success', timeout: 6000 },
        );
      } catch {
        // toast store not ready (e.g. tests without pinia) — silently skip
      }
    }, 160);
  }

  function applyTree(data: Partial<SpecTreeData>) {
    const nextNodes = data.nodes ?? [];
    maybeFireBootstrap(nextNodes);
    tree.value = nextNodes;
    treeIndex.value = data.index ?? null;
    treeProgress.value = data.progress ?? {};
    treeLoading.value = false;
  }

  async function fetchTree() {
    try {
      const data = await api<SpecTreeData>('GET', '/api/specs/tree');
      applyTree(data);
    } catch (e) {
      console.error('spec tree:', e);
      treeLoading.value = false;
    }
  }

  interface StaleCandidatesResponse {
    candidates: { path: string; files: string[]; reason: string }[];
  }

  async function fetchStaleCandidates() {
    try {
      const data = await api<StaleCandidatesResponse>('GET', '/api/specs/stale-candidates');
      const map: Record<string, { files: string[]; reason: string }> = {};
      for (const c of data.candidates ?? []) {
        map[c.path] = { files: c.files, reason: c.reason };
      }
      staleCandidates.value = map;
    } catch (e) {
      console.error('stale candidates:', e);
    }
  }

  function focusSpec(path: string) {
    focusedSpecPath.value = path;
    focusedIsIndex.value = false;
    focusedTaskId.value = '';
    focusedTaskTitle.value = '';
    focusedTaskPrompt.value = '';
  }

  function focusIndex() {
    focusedSpecPath.value = treeIndex.value?.path ?? '';
    focusedIsIndex.value = true;
    focusedTaskId.value = '';
    focusedTaskTitle.value = '';
    focusedTaskPrompt.value = '';
  }

  function clearFocus() {
    focusedSpecPath.value = '';
    focusedIsIndex.value = false;
    focusedTaskId.value = '';
    focusedTaskTitle.value = '';
    focusedTaskPrompt.value = '';
  }

  // openPlanForTask pins the Plan view to a specific task: focused-view
  // shows the task prompt, chat thread is activated (or created) for the
  // task. Mirrors ui/js/spec-mode.js openPlanForTask.
  async function openPlanForTask(taskId: string, title: string, prompt: string): Promise<void> {
    if (!taskId) return;
    focusedSpecPath.value = '';
    focusedIsIndex.value = false;
    focusedTaskId.value = taskId;
    focusedTaskTitle.value = title;
    focusedTaskPrompt.value = prompt;

    // Reuse an existing non-archived task-mode thread for this task.
    let match: PlanningThread | null = null;
    for (const id of threadOrder.value) {
      const t = threads.value[id];
      if (t && t.mode === 'task' && t.task_id === taskId) {
        match = t;
        break;
      }
    }
    if (match) {
      activeThreadId.value = match.id;
      api(
        'PATCH',
        '/api/planning/threads/' + encodeURIComponent(match.id),
        { state: 'active' },
      ).catch(() => {});
      return;
    }

    // Create a new task-mode thread pinned to this task.
    try {
      const created = await api<PlanningThread>('POST', '/api/planning/threads', {
        name: 'Task prompt: ' + (title || taskId),
        focused_task: taskId,
      });
      if (created?.id) {
        await api(
          'PATCH',
          '/api/planning/threads/' + encodeURIComponent(created.id),
          { state: 'active' },
        ).catch(() => {});
        await loadThreads();
        activeThreadId.value = created.id;
      }
    } catch (e) {
      console.error('openPlanForTask:', e);
    }
  }

  // ── Threads ────────────────────────────────────────────────────────

  interface ThreadListResponse {
    threads: Array<{
      id: string;
      name: string;
      archived: boolean;
      mode?: 'spec' | 'task';
      task_id?: string;
      created?: string;
      updated?: string;
    }>;
    active_id?: string;
    busy_thread_id?: string;
  }

  // refreshBusy updates busyThreadId and picks up in-place thread renames (e.g.
  // server-side auto-titling) from the threads endpoint, leaving the list order
  // and active selection untouched. Safe to poll on an interval (unlike
  // loadThreads, which reassigns activeThreadId and reloads history).
  async function refreshBusy() {
    try {
      const res = await api<ThreadListResponse>(
        'GET',
        '/api/planning/threads?includeArchived=true',
      );
      busyThreadId.value = res.busy_thread_id ?? '';
      for (const t of res.threads ?? []) {
        const cur = threads.value[t.id];
        if (!cur) continue;
        if (cur.name !== t.name) cur.name = t.name;
        const updated = parseTime(t.updated);
        if (updated && updated !== cur.updated) cur.updated = updated;
      }
    } catch {
      /* ignore transient poll failures */
    }
  }

  async function loadThreads() {
    try {
      const res = await api<ThreadListResponse>(
        'GET',
        '/api/planning/threads?includeArchived=true',
      );
      const all = res.threads ?? [];
      const next: Record<string, PlanningThread> = {};
      const order: string[] = [];
      const archived: PlanningThread[] = [];
      for (const t of all) {
        const prev = threads.value[t.id];
        const rec: PlanningThread = {
          id: t.id,
          name: t.name,
          archived: !!t.archived,
          mode: (t.mode as PlanningThread['mode']) ?? prev?.mode ?? '',
          task_id: t.task_id ?? prev?.task_id ?? '',
          unread: prev?.unread ?? false,
          scrollTop: prev?.scrollTop ?? 0,
          queue: prev?.queue ?? [],
          enqueuedAt: prev?.enqueuedAt ?? 0,
          lastViewedAt: prev?.lastViewedAt ?? 0,
          created: parseTime(t.created) || prev?.created || 0,
          updated: parseTime(t.updated) || prev?.updated || 0,
        };
        next[t.id] = rec;
        if (rec.archived) archived.push(rec);
        else order.push(rec.id);
      }
      threads.value = next;
      threadOrder.value = order;
      archivedThreads.value = archived;
      const wantActive = res.active_id ?? order[0] ?? '';
      activeThreadId.value = wantActive in next ? wantActive : (order[0] ?? '');
      busyThreadId.value = res.busy_thread_id ?? '';
    } catch (e) {
      console.error('planning threads:', e);
    }
  }

  return {
    tree, treeProgress, treeIndex, treeLoading,
    staleCandidates,
    focusedSpecPath, focusedIsIndex,
    focusedTaskId, focusedTaskTitle, focusedTaskPrompt,
    threads, threadOrder, archivedThreads, activeThreadId,
    streaming, streamingThreadId, busyThreadId,
    sortedNodes, nodesByPath, focusedNode,
    applyTree, fetchTree, fetchStaleCandidates, focusSpec, focusIndex, clearFocus,
    openPlanForTask,
    loadThreads, refreshBusy,
  };
});
