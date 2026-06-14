<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { storeToRefs } from 'pinia';
import { usePlanningStore } from '../stores/planning';
import type { SpecNode, SpecIndexMeta, SpecProgress } from '../stores/planning';
import { useTaskStore } from '../stores/tasks';
import { useSse } from '../composables/useSse';
import { watchThemeReinit } from '../lib/mermaidRender';
import SpecTreePanel from '../components/plan/SpecTreePanel.vue';
import SpecFocusedView from '../components/plan/SpecFocusedView.vue';
import PlanningChatPanel from '../components/plan/PlanningChatPanel.vue';

const route = useRoute();
const router = useRouter();
const planning = usePlanningStore();
const tasks = useTaskStore();
const { tree, treeIndex, treeLoading, focusedSpecPath } = storeToRefs(planning);

// ── Layout state machine ───────────────────────────────────────────
//
// Mirrors ui/js/spec-mode.js _applyLayout: chat-first when there are no
// specs and no Roadmap index, three-pane otherwise. Chat-first force-
// shows the chat pane.
//
// While the initial tree fetch is still in flight (treeLoading), keep
// the three-pane layout so users with specs don't see a flash of
// chat-first before their tree arrives.

const layout = computed<'chat-first' | 'three-pane'>(() => {
  if (treeLoading.value) return 'three-pane';
  const hasSpecs = tree.value.length > 0;
  const hasIndex = !!treeIndex.value;
  return hasSpecs || hasIndex ? 'three-pane' : 'chat-first';
});

// ── Chat pane visibility (persisted) ──────────────────────────────

const CHAT_OPEN_KEY = 'wallfacer-spec-chat-open';
const chatOpen = ref<boolean>(localStorage.getItem(CHAT_OPEN_KEY) !== '0');

function toggleChat() {
  chatOpen.value = !chatOpen.value;
  localStorage.setItem(CHAT_OPEN_KEY, chatOpen.value ? '1' : '0');
}

// In chat-first layout we force the chat to visible.
const effectiveChatOpen = computed(() => layout.value === 'chat-first' || chatOpen.value);

// ── Chat pane resize (persisted) ──────────────────────────────────

const CHAT_WIDTH_KEY = 'wallfacer-spec-chat-width';
const CHAT_MIN = 280;
const CHAT_MAX_FRAC = 0.5;

const chatWidth = ref<number>(parseInt(localStorage.getItem(CHAT_WIDTH_KEY) || '360', 10));

function startChatResize(ev: MouseEvent) {
  ev.preventDefault();
  const startX = ev.clientX;
  const startW = chatWidth.value;
  document.body.style.userSelect = 'none';
  document.body.style.cursor = 'col-resize';
  function onMove(mv: MouseEvent) {
    const delta = startX - mv.clientX;
    const maxW = Math.floor(window.innerWidth * CHAT_MAX_FRAC);
    chatWidth.value = Math.min(maxW, Math.max(CHAT_MIN, startW + delta));
  }
  function onUp() {
    document.removeEventListener('mousemove', onMove);
    document.removeEventListener('mouseup', onUp);
    document.body.style.userSelect = '';
    document.body.style.cursor = '';
    localStorage.setItem(CHAT_WIDTH_KEY, String(chatWidth.value));
  }
  document.addEventListener('mousemove', onMove);
  document.addEventListener('mouseup', onUp);
}

// ── Spec tree sidebar resize (persisted) ──────────────────────────

const SIDEBAR_WIDTH_KEY = 'wallfacer-spec-sidebar-width';
const SIDEBAR_MIN = 200;
const SIDEBAR_MAX = 520;

const sidebarWidth = ref<number>(parseInt(localStorage.getItem(SIDEBAR_WIDTH_KEY) || '280', 10));

function startSidebarResize(ev: MouseEvent) {
  ev.preventDefault();
  const startX = ev.clientX;
  const startW = sidebarWidth.value;
  document.body.style.userSelect = 'none';
  document.body.style.cursor = 'col-resize';
  function onMove(mv: MouseEvent) {
    const delta = mv.clientX - startX;
    sidebarWidth.value = Math.min(SIDEBAR_MAX, Math.max(SIDEBAR_MIN, startW + delta));
  }
  function onUp() {
    document.removeEventListener('mousemove', onMove);
    document.removeEventListener('mouseup', onUp);
    document.body.style.userSelect = '';
    document.body.style.cursor = '';
    localStorage.setItem(SIDEBAR_WIDTH_KEY, String(sidebarWidth.value));
  }
  document.addEventListener('mousemove', onMove);
  document.addEventListener('mouseup', onUp);
}

// ── Hash deep-link redirect ────────────────────────────────────────
//
// Old UI used #plan/<path> and #spec/<path> hash anchors to deep-link to
// a focused spec. The new router uses query-string focusing (`?spec=`),
// so on first mount we translate any legacy hash into the equivalent
// query and clear the hash.

function readHashPath(): string {
  const hash = window.location.hash || '';
  if (hash.startsWith('#plan/')) return decodeURIComponent(hash.slice('#plan/'.length));
  if (hash.startsWith('#spec/')) return decodeURIComponent(hash.slice('#spec/'.length));
  return '';
}

watch(focusedSpecPath, (path) => {
  // Keep the URL in sync so refresh preserves focus and shift+click into
  // the Map view round-trips back to the same spec.
  if (path) {
    void router.replace({ path: '/plan', query: { spec: path } });
  } else if (route.query.spec) {
    void router.replace({ path: '/plan' });
  }
});

// ── Cross-component focus from spec-link clicks ────────────────────

function focusSibling(path: string) {
  // Match against the loaded tree; if unknown, ignore (could be a docs link
  // we don't render in the tree).
  if (planning.tree.find(n => n.path === path)) {
    planning.focusSpec(path);
  } else {
    // Fall back to focusing anyway; loadCurrent will surface the error
    // state if the path is invalid.
    planning.focusSpec(path);
  }
}

// ── Forward Break Down chat send via the chat panel's expose ──────

const chatPanelRef = ref<{ send: (text: string) => void } | null>(null);

function sendChatFromHeader(text: string) {
  chatPanelRef.value?.send(text);
}

// ── Lifecycle ──────────────────────────────────────────────────────

onMounted(async () => {
  // The store needs config.workspaces[0] for the explorer/file API.
  if (!tasks.config) await tasks.fetchConfig();

  // Once mounted, re-render mermaid diagrams whenever the theme attribute
  // flips so SVGs stay legible against the new palette.
  watchThemeReinit();

  // Spec-tree fetch + SSE live on the page, not the tree panel, because
  // SpecTreePanel only mounts under the three-pane layout but the layout
  // itself is gated on tree.length > 0 || treeIndex. Without an
  // unconditional initial fetch the page would deadlock on chat-first.
  void planning.fetchTree();

  // Translate legacy hash deep-link.
  const hashPath = readHashPath();
  if (hashPath) {
    history.replaceState(null, '', window.location.pathname);
    void router.replace({ path: '/plan', query: { spec: hashPath } });
  }

  // Honour ?task=<id>: open task-mode planning pinned to that task.
  const focusTask = typeof route.query.task === 'string' ? route.query.task : '';
  if (focusTask) {
    if (tasks.tasks.length === 0) await tasks.fetchTasks().catch(() => {});
    const t = tasks.tasks.find(x => x.id === focusTask);
    void planning.openPlanForTask(focusTask, t?.title ?? '', t?.prompt ?? '');
  }

  // Honour ?spec=<path> when the tree finishes loading.
  const focus = typeof route.query.spec === 'string' ? route.query.spec : '';
  if (focus) {
    const stop = watch(tree, (v) => {
      if (v.length === 0) return;
      if (v.find(n => n.path === focus)) {
        planning.focusSpec(focus);
      }
      stop();
    }, { immediate: true });
  }
});

useSse({
  url: '/api/specs/stream',
  listeners: {
    snapshot(data: unknown) {
      if (data && typeof data === 'object') {
        const d = data as {
          nodes?: SpecNode[];
          index?: SpecIndexMeta | null;
          progress?: Record<string, SpecProgress>;
        };
        planning.applyTree({
          nodes: d.nodes ?? [],
          index: d.index ?? null,
          progress: d.progress ?? {},
        });
      }
    },
    heartbeat() { /* keepalive */ },
  },
});

// ── Keyboard shortcut: C toggles chat ─────────────────────────────

const focusedViewRef = ref<{ dispatchFocused: () => void; breakdownFocused: () => void } | null>(null);

function onKeydown(ev: KeyboardEvent) {
  if (ev.metaKey || ev.ctrlKey || ev.altKey) return;
  // Ignore when typing into a form field.
  const t = ev.target as HTMLElement;
  if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
  // c = toggle chat, d = dispatch focused spec, b = break it down (spec mode).
  if (ev.key === 'c' || ev.key === 'C') {
    if (layout.value === 'chat-first') return;
    ev.preventDefault();
    toggleChat();
  } else if (ev.key === 'd' || ev.key === 'D') {
    ev.preventDefault();
    focusedViewRef.value?.dispatchFocused();
  } else if (ev.key === 'b' || ev.key === 'B') {
    ev.preventDefault();
    focusedViewRef.value?.breakdownFocused();
  }
}

onMounted(() => window.addEventListener('keydown', onKeydown));
onUnmounted(() => window.removeEventListener('keydown', onKeydown));
</script>

<template>
  <div class="plan-page" :data-layout="layout">
    <SpecTreePanel
      v-if="layout === 'three-pane'"
      :style="{ '--stp-width': sidebarWidth + 'px' }"
    />
    <div
      v-if="layout === 'three-pane'"
      class="plan-resize-handle"
      role="separator"
      aria-orientation="vertical"
      @mousedown="startSidebarResize"
    />
    <SpecFocusedView
      v-if="layout === 'three-pane'"
      ref="focusedViewRef"
      :chat-visible="effectiveChatOpen"
      @toggle-chat="toggleChat"
      @focus-sibling="focusSibling"
      @send-chat="sendChatFromHeader"
    />
    <div
      v-if="layout === 'three-pane' && effectiveChatOpen"
      class="plan-resize-handle"
      role="separator"
      aria-orientation="vertical"
      @mousedown="startChatResize"
    />
    <PlanningChatPanel
      ref="chatPanelRef"
      :visible="effectiveChatOpen"
      :style="layout === 'three-pane' ? { width: chatWidth + 'px' } : undefined"
      :class="{ 'chat-first': layout === 'chat-first' }"
      @toggle="toggleChat"
    />
  </div>
</template>

<style scoped>
.plan-page {
  display: flex;
  height: 100%;
  overflow: hidden;
  background: var(--bg);
  color: var(--ink);
  font-family: var(--font-sans);
}

.plan-page[data-layout='chat-first'] {
  justify-content: center;
}

.plan-page[data-layout='chat-first'] :deep(.planning-chat-panel) {
  border-left: none;
  width: 100%;
  max-width: 720px;
}

.plan-resize-handle {
  width: 4px;
  background: transparent;
  cursor: col-resize;
  flex-shrink: 0;
  position: relative;
}

.plan-resize-handle:hover {
  background: var(--rule);
}
</style>
