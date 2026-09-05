<script setup lang="ts">
// SessionList — the vertical session sub-sidebar for the dedicated Chat view.
// Renders the workspace group's agent sessions as the "sessions" of this
// surface (the same threads the legacy panel showed as tabs), with active
// highlight, unread dots, inline rename, archive, a running spinner, and an
// archived overflow.
import { computed, onMounted, onUnmounted } from 'vue';
import { storeToRefs } from 'pinia';
import { useAgentStore } from '../../stores/agentSession';
import { useNow } from '../../composables/useNow';
import { bucketForUpdated, SESSION_BUCKETS } from './sessionBuckets';
import type { ChatSession } from '../../composables/useChatSession';

const props = defineProps<{ session: ChatSession }>();
// Folding the list to a rail is owned by the parent (ChatPage), which controls
// the layout slot; this panel only asks to be collapsed. Mirrors SpecTreePanel.
const emit = defineEmits<{ collapse: [] }>();
const s = props.session;

const agentStore = useAgentStore();
const {
  threads, threadOrder, archivedThreads, activeThreadId,
  streaming, streamingThreadId, busyThreadId,
} = storeToRefs(agentStore);

// A session shows a spinner while an agent turn is in flight on it. busyThreadId
// is the server's truth (so a session running in the background still spins
// while you view another); streamingThreadId gives instant local feedback for
// the session you just sent to, before the next poll lands.
function isRunning(id: string): boolean {
  return id === busyThreadId.value || (streaming.value && id === streamingThreadId.value);
}

// Group sessions by recency of last activity (the server's `updated` time):
// Today / Previous 7 days / Previous 30 days / Older. Within each bucket the
// most recently active session comes first. Running/unread state still shows
// per-row (spinner, unread dot); a running session is being touched now, so it
// naturally floats to the top of Today. Only non-empty buckets render.
//
// `now` ticks each second so buckets re-settle across a day boundary without a
// reload (cheap: only re-renders when a session actually changes bucket).
const now = useNow();
const sessionGroups = computed(() => {
  const sorted = [...threadOrder.value].sort(
    (a, b) => (threads.value[b]?.updated ?? 0) - (threads.value[a]?.updated ?? 0),
  );
  const byBucket = new Map<string, string[]>();
  for (const id of sorted) {
    const t = threads.value[id];
    if (!t) continue;
    const key = bucketForUpdated(now.value, t.updated || 0);
    (byBucket.get(key) ?? byBucket.set(key, []).get(key)!).push(id);
  }
  return SESSION_BUCKETS.flatMap(({ key, label }) => {
    const ids = byBucket.get(key);
    return ids?.length ? [{ key, label, ids }] : [];
  });
});

// Poll the server's busy thread on a light interval so background activity is
// reflected without disturbing the thread list or active selection.
let busyTimer: ReturnType<typeof setInterval> | null = null;
onMounted(() => {
  void agentStore.refreshBusy();
  busyTimer = setInterval(() => void agentStore.refreshBusy(), 3000);
});
onUnmounted(() => {
  if (busyTimer !== null) clearInterval(busyTimer);
});
</script>

<template>
  <aside class="chat-sessions">
    <div class="chat-sessions-bar">
      <span class="eyebrow chat-sessions-bar-title">Chats</span>
      <button
        type="button"
        class="chat-sessions-collapse"
        title="Collapse sessions"
        aria-label="Collapse sessions"
        @click="emit('collapse')"
      >
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="15 18 9 12 15 6"></polyline>
        </svg>
      </button>
    </div>

    <button
      type="button"
      class="btn ghost block chat-session-new"
      :class="{ 'chat-session-new--active': s.draft.value }"
      @click="s.createThread"
    >
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><line x1="12" y1="5" x2="12" y2="19"></line><line x1="5" y1="12" x2="19" y2="12"></line></svg>
      <span>New chat</span>
    </button>

    <div v-scrollfade class="chat-session-scroll">
      <template v-for="group in sessionGroups" :key="group.key">
        <div class="chat-sessions-head" :class="'chat-sessions-head--' + group.key">
          <span class="eyebrow chat-sessions-title">{{ group.label }}</span>
          <span class="count chat-sessions-count">{{ group.ids.length }}</span>
        </div>
        <div
          v-for="id in group.ids"
          :key="id"
          class="chat-session-row"
          :class="{ 'chat-session-row--active': id === activeThreadId }"
          role="button"
          tabindex="0"
          @click="s.switchToThread(id)"
          @keydown.enter="s.switchToThread(id)"
        >
          <input
            v-if="s.renamingId.value === id"
            v-model="s.renameDraft.value"
            class="chat-session-rename"
            type="text"
            @keydown.enter.prevent="s.commitRename"
            @keydown.escape.prevent="s.cancelRename"
            @blur="s.commitRename"
            @click.stop
          />
          <template v-else>
            <span class="chat-session-name">{{ threads[id]?.name }}</span>
            <span
              v-if="isRunning(id)"
              class="chat-session-spinner"
              role="status"
              aria-label="Agent running"
              title="Agent running"
            />
            <span v-else-if="id !== activeThreadId && threads[id]?.unread" class="chat-session-unread" />
            <span class="chat-session-actions">
              <button
                type="button"
                class="chat-session-btn"
                title="Rename"
                @click.stop="s.startRename(id)"
              >✎</button>
              <button
                type="button"
                class="chat-session-btn"
                title="Archive session"
                aria-label="Archive session"
                @click.stop="s.archiveThread(id)"
              >
                <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
                </svg>
              </button>
            </span>
          </template>
        </div>
      </template>
    </div>

    <div v-if="archivedThreads.length > 0" class="chat-sessions-archived">
      <button
        type="button"
        class="chat-sessions-archived-trigger"
        @click="s.archiveMenuOpen.value = !s.archiveMenuOpen.value"
      >
        Archived ({{ archivedThreads.length }}) <span>{{ s.archiveMenuOpen.value ? '▾' : '▸' }}</span>
      </button>
      <div v-if="s.archiveMenuOpen.value" class="chat-sessions-archived-list">
        <div
          v-for="t in archivedThreads"
          :key="t.id"
          class="chat-sessions-archived-row"
        >
          <button
            type="button"
            class="chat-sessions-archived-item"
            title="Restore session"
            @click="s.unarchiveThread(t.id)"
          >{{ t.name }}</button>
          <button
            type="button"
            class="chat-sessions-archived-delete"
            title="Delete permanently"
            aria-label="Delete session permanently"
            @click="s.deleteThread(t.id)"
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
            </svg>
          </button>
        </div>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.chat-sessions {
  width: var(--chat-sessions-width, 248px);
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
  border-right: 1px solid var(--rule);
  background: var(--bg-sunk);
  padding: 10px 8px;
}
.chat-sessions-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 4px 0 10px;
  min-height: 24px;
}
.chat-sessions-collapse {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  background: transparent;
  border: none;
  border-radius: var(--r-sm);
  color: var(--ink-3);
  cursor: pointer;
}
.chat-sessions-collapse:hover {
  color: var(--accent);
  background: var(--accent-soft);
}
.chat-session-new {
  justify-content: flex-start;
  gap: 8px;
  background: transparent;
  border-style: dashed;
  color: var(--ink-3);
}
.chat-session-new:hover,
.chat-session-new--active {
  color: var(--accent);
  border-style: solid;
}
.chat-sessions-head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 12px 10px 4px;
}
.chat-sessions-count {
  margin-left: 0;
}
.chat-session-scroll {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
/* A session row is the rail's nav row: 34px, radius 12, the active one on
   the card surface. */
.chat-session-row {
  display: flex;
  align-items: center;
  gap: 6px;
  min-height: 34px;
  padding: 0 10px;
  border-radius: var(--r-row);
  font-size: var(--fs-base);
  font-weight: 500;
  color: var(--ink-2);
  cursor: pointer;
  transition: background var(--dur-hover), color var(--dur-hover);
}
.chat-session-row:hover {
  background: color-mix(in srgb, var(--ink) 5%, transparent);
  color: var(--ink);
}
.chat-session-row--active {
  background: var(--bg-card);
  color: var(--ink);
  font-weight: 600;
  box-shadow: var(--sh-card);
}
.chat-session-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.chat-session-unread {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--accent);
  flex-shrink: 0;
}
.chat-session-spinner {
  width: 11px;
  height: 11px;
  flex-shrink: 0;
  border-radius: 50%;
  border: 2px solid var(--tint-blue);
  border-top-color: var(--run);
  animation: chat-session-spin 0.7s linear infinite;
}
@keyframes chat-session-spin {
  to { transform: rotate(360deg); }
}
@media (prefers-reduced-motion: reduce) {
  .chat-session-spinner { animation-duration: 2s; }
}
.chat-session-actions {
  display: none;
  gap: 2px;
}
.chat-session-row:hover .chat-session-actions {
  display: inline-flex;
}
.chat-session-btn {
  display: inline-flex;
  align-items: center;
  background: none;
  border: none;
  cursor: pointer;
  color: var(--ink-4);
  font-size: 12px;
  padding: 0 2px;
}
.chat-session-btn:hover {
  color: var(--ink);
}
.chat-session-rename {
  flex: 1;
  font-size: var(--fs-base);
  padding: 2px 6px;
  border: 1px solid var(--accent-line);
  background: var(--bg-card);
  color: var(--ink);
  border-radius: var(--r-xs);
  outline: none;
}
.chat-sessions-archived {
  border-top: 1px solid var(--rule);
  margin: 6px -8px 0;
  padding: 8px 8px 0;
}
.chat-sessions-archived-trigger {
  width: 100%;
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 10px;
  background: transparent;
  border: none;
  border-radius: var(--r-sm);
  font: 500 var(--fs-10) / 1 var(--font-mono);
  color: var(--ink-3);
  cursor: pointer;
}
.chat-sessions-archived-trigger:hover {
  color: var(--ink);
  background: color-mix(in srgb, var(--ink) 5%, transparent);
}
.chat-sessions-archived-list {
  display: flex;
  flex-direction: column;
}
.chat-sessions-archived-row {
  display: flex;
  align-items: center;
  border-radius: var(--r-sm);
}
.chat-sessions-archived-row:hover {
  background: color-mix(in srgb, var(--ink) 5%, transparent);
}
.chat-sessions-archived-item {
  flex: 1;
  min-width: 0;
  display: block;
  padding: 6px 10px;
  font-size: 12px;
  text-align: left;
  background: transparent;
  border: none;
  color: var(--ink-2);
  cursor: pointer;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.chat-sessions-archived-row:hover .chat-sessions-archived-item {
  color: var(--ink);
}
.chat-sessions-archived-delete {
  display: none;
  align-items: center;
  flex-shrink: 0;
  padding: 4px 8px;
  background: transparent;
  border: none;
  color: var(--ink-4);
  cursor: pointer;
}
.chat-sessions-archived-row:hover .chat-sessions-archived-delete {
  display: inline-flex;
}
.chat-sessions-archived-delete:hover {
  color: var(--err);
}
</style>
