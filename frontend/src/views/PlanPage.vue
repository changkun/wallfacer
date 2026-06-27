<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { storeToRefs } from 'pinia';
import { useAgentStore } from '../stores/agentSession';
import type { SpecNode, SpecIndexMeta, SpecProgress } from '../stores/agentSession';
import { useTaskStore } from '../stores/tasks';
import { useSse } from '../composables/useSse';
import { watchThemeReinit } from '../lib/mermaidRender';
import SpecTreePanel from '../components/plan/SpecTreePanel.vue';
import SpecFocusedView from '../components/plan/SpecFocusedView.vue';
import AgentChatPanel from '../components/plan/AgentChatPanel.vue';
import SpecChatPopup from '../components/plan/SpecChatPopup.vue';

const route = useRoute();
const router = useRouter();
const agentStore = useAgentStore();
const tasks = useTaskStore();
const { tree, treeIndex, treeLoading, focusedSpecPath } = storeToRefs(agentStore);

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

// ── Floating chat popup (three-pane) ──────────────────────────────
//
// In three-pane mode the chat is a floating, draggable popup that overlays the
// focused view (SpecChatPopup owns its own open/position/size persistence). The
// page only needs a ref to toggle it (C shortcut, focused-view chat button) and
// to forward Break Down sends. In chat-first mode the popup isn't mounted; the
// full-width AgentChatPanel covers the empty-workspace case.
//
// Gates the three-pane agent chat: the floating popup, its focused-view
// toggle button, and the C shortcut. Set false to hide chat in Plan mode
// (the dedicated /chat surface is unaffected); true shows the popup launcher
// in the bottom-right. The chat-first empty-workspace onboarding is unaffected
// either way (it's the only content when there are no specs).
const AGENT_CHAT_ENABLED = true;

const popupRef = ref<{ toggle: () => void; open: () => void; isOpen: boolean; send: (t: string) => void } | null>(null);

function toggleChat() {
  popupRef.value?.toggle();
}

const chatVisible = computed(() => !!popupRef.value?.isOpen);

// ── Spec tree sidebar resize (persisted) ──────────────────────────

const SIDEBAR_WIDTH_KEY = 'wallfacer-spec-sidebar-width';
const SIDEBAR_MIN = 200;
const SIDEBAR_MAX = 520;

const sidebarWidth = ref<number>(parseInt(localStorage.getItem(SIDEBAR_WIDTH_KEY) || '280', 10));

// Collapse the spec tree to a thin rail, mirroring the Board's file explorer:
// when folded, a 28px left-edge strip advertises that the tree can be reopened.
// Persisted so the choice survives reloads.
const SIDEBAR_COLLAPSED_KEY = 'wallfacer-spec-tree-collapsed';
const sidebarCollapsed = ref<boolean>(localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === '1');

function collapseSidebar() {
  sidebarCollapsed.value = true;
  localStorage.setItem(SIDEBAR_COLLAPSED_KEY, '1');
}
function expandSidebar() {
  sidebarCollapsed.value = false;
  localStorage.setItem(SIDEBAR_COLLAPSED_KEY, '0');
}

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
  if (agentStore.tree.find(n => n.path === path)) {
    agentStore.focusSpec(path);
  } else {
    // Fall back to focusing anyway; loadCurrent will surface the error
    // state if the path is invalid.
    agentStore.focusSpec(path);
  }
}

// ── Forward Break Down chat send into the floating popup ──────────
// SpecFocusedView (three-pane only) emits @send-chat for its Break Down
// button; route it into the popup, opening it if collapsed.

function sendChatFromHeader(text: string) {
  popupRef.value?.send(text);
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
  void agentStore.fetchTree();

  // Translate legacy hash deep-link.
  const hashPath = readHashPath();
  if (hashPath) {
    history.replaceState(null, '', window.location.pathname);
    void router.replace({ path: '/plan', query: { spec: hashPath } });
  }

  // Honour ?task=<id>: open task-mode agent session pinned to that task.
  const focusTask = typeof route.query.task === 'string' ? route.query.task : '';
  if (focusTask) {
    if (tasks.tasks.length === 0) await tasks.fetchTasks().catch(() => {});
    const t = tasks.tasks.find(x => x.id === focusTask);
    void agentStore.openPlanForTask(focusTask, t?.title ?? '', t?.prompt ?? '');
  }

  // Honour ?spec=<path> when the tree finishes loading.
  const focus = typeof route.query.spec === 'string' ? route.query.spec : '';
  if (focus) {
    const stop = watch(tree, (v) => {
      if (v.length === 0) return;
      if (v.find(n => n.path === focus)) {
        agentStore.focusSpec(focus);
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
        agentStore.applyTree({
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
    if (!AGENT_CHAT_ENABLED || layout.value === 'chat-first') return;
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
    <template v-if="layout === 'three-pane'">
      <template v-if="!sidebarCollapsed">
        <SpecTreePanel
          :style="{ '--stp-width': sidebarWidth + 'px' }"
          @collapse="collapseSidebar"
        />
        <div
          class="plan-resize-handle"
          role="separator"
          aria-orientation="vertical"
          @mousedown="startSidebarResize"
        />
      </template>
      <!-- Collapsed rail: a persistent left-edge affordance to reopen the spec
           tree, occupying the same slot the panel would. Mirrors the Board's
           explorer rail. -->
      <button
        v-else
        type="button"
        class="spec-tree-rail"
        title="Show spec tree"
        aria-label="Show spec tree"
        @click="expandSidebar"
      >
        <svg
          class="spec-tree-rail__icon"
          width="18"
          height="18"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <line x1="8" y1="6" x2="21" y2="6"></line>
          <line x1="8" y1="12" x2="21" y2="12"></line>
          <line x1="8" y1="18" x2="21" y2="18"></line>
          <line x1="3" y1="6" x2="3.01" y2="6"></line>
          <line x1="3" y1="12" x2="3.01" y2="12"></line>
          <line x1="3" y1="18" x2="3.01" y2="18"></line>
        </svg>
        <svg
          class="spec-tree-rail__chevron"
          width="13"
          height="13"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <polyline points="9 18 15 12 9 6"></polyline>
        </svg>
      </button>
    </template>
    <SpecFocusedView
      v-if="layout === 'three-pane'"
      ref="focusedViewRef"
      :chat-visible="chatVisible"
      :chat-enabled="AGENT_CHAT_ENABLED"
      @toggle-chat="toggleChat"
      @focus-sibling="focusSibling"
      @send-chat="sendChatFromHeader"
    />
    <!-- Three-pane: chat floats over the focused view (deactivated). -->
    <SpecChatPopup v-if="layout === 'three-pane' && AGENT_CHAT_ENABLED" ref="popupRef" />
    <!-- Chat-first: no specs yet, the full-width panel covers the workspace.
         Gated on the layout directly (not v-else) so deactivating the
         three-pane popup above doesn't pull this panel into the spec view. -->
    <AgentChatPanel
      v-if="layout === 'chat-first'"
      :visible="true"
      class="chat-first"
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

.plan-page[data-layout='chat-first'] :deep(.agent-chat-panel) {
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

/* Collapsed rail: persistent left-edge strip that reopens the spec tree,
   matching the Board explorer's collapsed rail. */
.spec-tree-rail {
  flex-shrink: 0;
  width: 28px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 7px;
  padding: 9px 0;
  border: none;
  border-right: 1px solid var(--rule);
  background: var(--bg-card);
  color: var(--ink-3);
  cursor: pointer;
}

.spec-tree-rail:hover {
  color: var(--accent);
  background: var(--bg-hover);
}

.spec-tree-rail__chevron {
  opacity: 0.55;
}
</style>
