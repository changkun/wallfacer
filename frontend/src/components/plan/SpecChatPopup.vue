<script setup lang="ts">
// SpecChatPopup — the planning chat as a floating, draggable, resizable popup
// that hovers over the focused spec view (Plan page). Replaces the old docked
// chat column so the spec tree and focused view reclaim the full width. Open
// state, position, and size persist to localStorage. Built from the shared chat
// core; the popup adds only its floating chrome and a compact session switcher.
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue';
import { storeToRefs } from 'pinia';
import { usePlanningStore } from '../../stores/planning';
import { useChatSession } from '../../composables/useChatSession';
import ChatMessageList from './ChatMessageList.vue';
import ChatComposer from './ChatComposer.vue';

const planning = usePlanningStore();
const { threads, threadOrder, activeThreadId } = storeToRefs(planning);

const chat = useChatSession();

// ── Persisted geometry + open state ────────────────────────────────
const STORE_KEY = 'wallfacer-spec-chat-popup';
const MARGIN = 16;
const MIN_W = 300, MIN_H = 320;
const DEFAULT_W = 380, DEFAULT_H = 520;

interface PopupState { x: number; y: number; w: number; h: number; open: boolean }

const geom = reactive<PopupState>(loadState());

function loadState(): PopupState {
  let saved: Partial<PopupState> = {};
  try {
    saved = JSON.parse(localStorage.getItem(STORE_KEY) || '{}') ?? {};
  } catch { /* ignore */ }
  const w = clampNum(saved.w ?? DEFAULT_W, MIN_W, vw());
  const h = clampNum(saved.h ?? DEFAULT_H, MIN_H, vh());
  // Default anchor: bottom-right.
  const x = saved.x ?? Math.max(MARGIN, vw() - w - MARGIN);
  const y = saved.y ?? Math.max(MARGIN, vh() - h - MARGIN);
  return { w, h, x: clampX(x, w), y: clampY(y, h), open: saved.open ?? false };
}

function persist() {
  try { localStorage.setItem(STORE_KEY, JSON.stringify(geom)); } catch { /* ignore */ }
}

function vw() { return typeof window !== 'undefined' ? window.innerWidth : 1280; }
function vh() { return typeof window !== 'undefined' ? window.innerHeight : 800; }
function clampNum(v: number, lo: number, hi: number) { return Math.min(Math.max(v, lo), Math.max(lo, hi)); }
function clampX(x: number, w: number) { return clampNum(x, 0, Math.max(0, vw() - w)); }
function clampY(y: number, h: number) { return clampNum(y, 0, Math.max(0, vh() - h)); }

function reclamp() {
  geom.w = clampNum(geom.w, MIN_W, vw());
  geom.h = clampNum(geom.h, MIN_H, vh());
  geom.x = clampX(geom.x, geom.w);
  geom.y = clampY(geom.y, geom.h);
}

const popupStyle = computed(() => ({
  left: geom.x + 'px',
  top: geom.y + 'px',
  width: geom.w + 'px',
  height: geom.h + 'px',
}));

// ── Open / close ───────────────────────────────────────────────────
function toggle() { geom.open = !geom.open; persist(); }
function open() { if (!geom.open) { geom.open = true; persist(); } }

// ── Drag ───────────────────────────────────────────────────────────
let dragStart: { px: number; py: number; ox: number; oy: number } | null = null;
function onDragDown(ev: PointerEvent) {
  if ((ev.target as HTMLElement).closest('button, .scp-session-switch')) return;
  dragStart = { px: ev.clientX, py: ev.clientY, ox: geom.x, oy: geom.y };
  window.addEventListener('pointermove', onDragMove);
  window.addEventListener('pointerup', onDragUp);
  ev.preventDefault();
}
function onDragMove(ev: PointerEvent) {
  if (!dragStart) return;
  geom.x = clampX(dragStart.ox + (ev.clientX - dragStart.px), geom.w);
  geom.y = clampY(dragStart.oy + (ev.clientY - dragStart.py), geom.h);
}
function onDragUp() {
  dragStart = null;
  window.removeEventListener('pointermove', onDragMove);
  window.removeEventListener('pointerup', onDragUp);
  persist();
}

// ── Resize (bottom-right corner) ───────────────────────────────────
let resizeStart: { px: number; py: number; ow: number; oh: number } | null = null;
function onResizeDown(ev: PointerEvent) {
  resizeStart = { px: ev.clientX, py: ev.clientY, ow: geom.w, oh: geom.h };
  window.addEventListener('pointermove', onResizeMove);
  window.addEventListener('pointerup', onResizeUp);
  ev.preventDefault();
  ev.stopPropagation();
}
function onResizeMove(ev: PointerEvent) {
  if (!resizeStart) return;
  geom.w = clampNum(resizeStart.ow + (ev.clientX - resizeStart.px), MIN_W, vw() - geom.x);
  geom.h = clampNum(resizeStart.oh + (ev.clientY - resizeStart.py), MIN_H, vh() - geom.y);
}
function onResizeUp() {
  resizeStart = null;
  window.removeEventListener('pointermove', onResizeMove);
  window.removeEventListener('pointerup', onResizeUp);
  persist();
}

// ── Compact session switcher ───────────────────────────────────────
const switcherOpen = ref(false);
const activeName = computed(() => (activeThreadId.value && threads.value[activeThreadId.value]?.name) || 'Chat');
function pickSession(id: string) {
  switcherOpen.value = false;
  void chat.switchToThread(id);
}
function newSession() {
  switcherOpen.value = false;
  void chat.createThread();
}

onMounted(() => window.addEventListener('resize', reclamp));
onUnmounted(() => window.removeEventListener('resize', reclamp));

// Close the switcher dropdown on any outside click while it's open.
watch(switcherOpen, (isOpen) => {
  if (!isOpen) return;
  const handler = (e: MouseEvent) => {
    if (!(e.target as HTMLElement).closest('.scp-session')) {
      switcherOpen.value = false;
      document.removeEventListener('mousedown', handler);
    }
  };
  setTimeout(() => document.addEventListener('mousedown', handler), 0);
});

defineExpose({
  toggle,
  open,
  isOpen: computed(() => geom.open),
  send(text: string) { open(); void chat.sendMessage(text); },
});
</script>

<template>
  <div class="spec-chat-popup-root">
    <!-- Collapsed launcher -->
    <button
      v-if="!geom.open"
      type="button"
      class="scp-launcher"
      title="Open planning chat (C)"
      @click="toggle"
    >
      <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M21 11.5a8.38 8.38 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.38 8.38 0 0 1-3.8-.9L3 21l1.9-5.7a8.38 8.38 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.38 8.38 0 0 1 3.8-.9h.5a8.48 8.48 0 0 1 8 8v.5z"></path></svg>
    </button>

    <!-- Floating popup -->
    <section
      v-show="geom.open"
      class="scp-window"
      :style="popupStyle"
      aria-label="Planning chat"
    >
      <header class="scp-header" @pointerdown="onDragDown">
        <span class="scp-grip" aria-hidden="true">⠿</span>
        <div class="scp-session">
          <button
            type="button"
            class="scp-session-switch"
            :title="'Session: ' + activeName"
            @click="switcherOpen = !switcherOpen"
          >
            <span class="scp-session-name">{{ activeName }}</span>
            <span class="scp-session-caret">▾</span>
          </button>
          <div v-if="switcherOpen" class="scp-session-menu" role="menu">
            <button
              v-for="id in threadOrder"
              :key="id"
              type="button"
              class="scp-session-item"
              :class="{ 'scp-session-item--active': id === activeThreadId }"
              role="menuitem"
              @click="pickSession(id)"
            >
              <span class="scp-session-item-name">{{ threads[id]?.name }}</span>
              <span v-if="id !== activeThreadId && threads[id]?.unread" class="scp-session-unread" />
            </button>
            <div class="scp-session-divider" />
            <button type="button" class="scp-session-item scp-session-new" role="menuitem" @click="newSession">
              + New chat
            </button>
          </div>
        </div>
        <div class="scp-header-actions">
          <button type="button" class="scp-iconbtn" title="Clear conversation" @click="chat.clearHistory">⌫</button>
          <button type="button" class="scp-iconbtn" title="Hide chat (C)" @click="toggle">✕</button>
        </div>
      </header>

      <ChatMessageList :session="chat" />

      <ChatComposer
        :streaming="chat.streaming.value"
        variant="compact"
        @send="chat.sendMessage"
        @interrupt="chat.onInterrupt"
      />

      <span class="scp-resize" title="Resize" @pointerdown="onResizeDown" />
    </section>
  </div>
</template>

<style scoped>
.scp-launcher {
  position: fixed;
  right: 20px;
  bottom: 20px;
  width: 48px;
  height: 48px;
  border-radius: 50%;
  background: var(--accent);
  color: #fff;
  border: none;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.25);
  z-index: 40;
  transition: transform 120ms ease;
}
.scp-launcher:hover { transform: scale(1.06); }

.scp-window {
  position: fixed;
  display: flex;
  flex-direction: column;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-lg, 14px);
  box-shadow: 0 12px 40px rgba(0, 0, 0, 0.35);
  overflow: hidden;
  z-index: 41;
  animation: scp-pop 200ms cubic-bezier(0.2, 0, 0, 1);
}

@keyframes scp-pop {
  from { opacity: 0; transform: scale(0.96); }
  to { opacity: 1; transform: scale(1); }
}

.scp-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  border-bottom: 1px solid var(--rule);
  cursor: grab;
  user-select: none;
}
.scp-header:active { cursor: grabbing; }

.scp-grip {
  color: var(--ink-4);
  font-size: 12px;
  letter-spacing: -2px;
}

.scp-session {
  position: relative;
  flex: 1;
  min-width: 0;
}

.scp-session-switch {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 100%;
  padding: 3px 8px;
  background: transparent;
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  color: var(--ink);
  font-size: 12px;
  font-family: inherit;
  cursor: pointer;
}
.scp-session-switch:hover { background: var(--bg-hover); }

.scp-session-name {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.scp-session-caret { color: var(--ink-3); font-size: 9px; }

.scp-session-menu {
  position: absolute;
  top: calc(100% + 4px);
  left: 0;
  min-width: 200px;
  max-height: 280px;
  overflow-y: auto;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2);
  z-index: 5;
  padding: 4px;
}

.scp-session-item {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 6px 8px;
  background: transparent;
  border: none;
  border-radius: var(--r-sm);
  color: var(--ink-2);
  font-size: 12px;
  text-align: left;
  cursor: pointer;
}
.scp-session-item:hover { background: var(--bg-hover); color: var(--ink); }
.scp-session-item--active { background: var(--bg-active); color: var(--ink); }

.scp-session-item-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.scp-session-unread {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent);
}

.scp-session-divider {
  height: 1px;
  background: var(--rule);
  margin: 4px 0;
}

.scp-session-new { color: var(--accent); }

.scp-header-actions {
  display: flex;
  gap: 2px;
}

.scp-iconbtn {
  width: 24px;
  height: 24px;
  background: transparent;
  border: none;
  border-radius: var(--r-sm);
  color: var(--ink-3);
  font-size: 13px;
  cursor: pointer;
}
.scp-iconbtn:hover { background: var(--bg-hover); color: var(--ink); }

.scp-resize {
  position: absolute;
  right: 0;
  bottom: 0;
  width: 16px;
  height: 16px;
  cursor: nwse-resize;
  background:
    linear-gradient(135deg, transparent 50%, var(--ink-4) 50%, var(--ink-4) 60%, transparent 60%, transparent 70%, var(--ink-4) 70%, var(--ink-4) 80%, transparent 80%);
  opacity: 0.5;
}
.scp-resize:hover { opacity: 0.9; }

@media (prefers-reduced-motion: reduce) {
  .scp-window { animation: none; }
  .scp-launcher { transition: none; }
}
</style>
