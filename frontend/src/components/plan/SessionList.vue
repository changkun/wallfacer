<script setup lang="ts">
// SessionList — the vertical session sub-sidebar for the dedicated Chat view.
// Renders the workspace group's planning threads as the "sessions" of this
// surface (the same threads the legacy panel showed as tabs), with active
// highlight, unread dots, inline rename, archive, a running spinner, and an
// archived overflow.
import { computed, onMounted, onUnmounted } from 'vue';
import { storeToRefs } from 'pinia';
import { usePlanningStore } from '../../stores/planning';
import { useNow } from '../../composables/useNow';
import { bucketForUpdated, SESSION_BUCKETS } from './sessionBuckets';
import type { ChatSession } from '../../composables/useChatSession';

const props = defineProps<{ session: ChatSession }>();
const s = props.session;

const planning = usePlanningStore();
const {
  threads, threadOrder, archivedThreads, activeThreadId,
  streaming, streamingThreadId, busyThreadId,
} = storeToRefs(planning);

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
  void planning.refreshBusy();
  busyTimer = setInterval(() => void planning.refreshBusy(), 3000);
});
onUnmounted(() => {
  if (busyTimer !== null) clearInterval(busyTimer);
});
</script>

<template>
  <aside class="chat-sessions">
    <button
      type="button"
      class="chat-session-new"
      :class="{ 'chat-session-new--active': s.draft.value }"
      @click="s.createThread"
    >
      <span class="chat-session-new-icon" aria-hidden="true">+</span>
      <span>New chat</span>
    </button>

    <div class="chat-session-scroll">
      <template v-for="group in sessionGroups" :key="group.key">
        <div class="chat-sessions-head" :class="'chat-sessions-head--' + group.key">
          <span class="chat-sessions-title">{{ group.label }}</span>
          <span class="chat-sessions-count">{{ group.ids.length }}</span>
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
  width: 248px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--rule);
  background: var(--bg-card);
  padding: 8px;
}

/* New chat — a clean, borderless row aligned with the session list, with a
   muted leading +; matches the row geometry below rather than a boxed button. */
.chat-session-new {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 8px 10px;
  font-size: 13px;
  font-weight: 500;
  background: transparent;
  border: none;
  border-radius: var(--r-md);
  color: var(--ink);
  cursor: pointer;
  text-align: left;
}

.chat-session-new:hover {
  background: var(--bg-hover);
}

/* Active while an unsent "New chat" draft is open. */
.chat-session-new--active {
  background: var(--bg-active);
  font-weight: 500;
}

.chat-session-new-icon {
  font-size: 16px;
  line-height: 1;
  color: var(--ink-3);
  width: 16px;
  text-align: center;
}

.chat-sessions-head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 12px 10px 4px;
}

.chat-sessions-title {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.05em;
  color: var(--ink-4);
}

.chat-sessions-count {
  font-size: 10px;
  font-weight: 600;
  color: var(--ink-3);
  background: var(--bg-sunk);
  border-radius: 999px;
  padding: 0 6px;
  min-width: 16px;
  text-align: center;
}

.chat-session-scroll {
  flex: 1;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.chat-session-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 10px;
  border-radius: var(--r-md);
  font-size: 13px;
  color: var(--ink-2);
  cursor: pointer;
  transition: background 0.1s, color 0.1s;
}

.chat-session-row:hover {
  background: var(--bg-hover);
}

.chat-session-row--active {
  background: var(--bg-active);
  color: var(--ink);
  font-weight: 500;
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
  border: 2px solid color-mix(in oklab, var(--accent) 30%, transparent);
  border-top-color: var(--accent);
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
  font-size: 13px;
  padding: 2px 6px;
  border: 1px solid var(--accent);
  background: var(--bg);
  color: var(--ink);
  border-radius: 4px;
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
  padding: 6px 8px;
  background: transparent;
  border: none;
  font-size: 11px;
  color: var(--ink-3);
  cursor: pointer;
}

.chat-sessions-archived-trigger:hover {
  color: var(--ink);
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
  background: var(--bg-hover);
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
  color: var(--danger, #c0392b);
}
</style>
