<script setup lang="ts">
// SessionList — the vertical session sub-sidebar for the dedicated Chat view.
// Renders the workspace group's planning threads as the "sessions" of this
// surface (the same threads the legacy panel showed as tabs), with active
// highlight, unread dots, inline rename, archive, and an archived overflow.
import { storeToRefs } from 'pinia';
import { usePlanningStore } from '../../stores/planning';
import type { ChatSession } from '../../composables/useChatSession';

const props = defineProps<{ session: ChatSession }>();
const s = props.session;

const planning = usePlanningStore();
const { threads, threadOrder, archivedThreads, activeThreadId } = storeToRefs(planning);
</script>

<template>
  <aside class="chat-sessions">
    <div class="chat-sessions-head">
      <span class="chat-sessions-title">Sessions</span>
    </div>

    <button
      type="button"
      class="chat-session-new"
      @click="s.createThread"
    >
      <span class="chat-session-new-icon">+</span> New chat
    </button>

    <div class="chat-session-scroll">
      <div
        v-for="id in threadOrder"
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
          <span v-if="id !== activeThreadId && threads[id]?.unread" class="chat-session-unread" />
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
              @click.stop="s.archiveThread(id)"
            >×</button>
          </span>
        </template>
      </div>
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
        <button
          v-for="t in archivedThreads"
          :key="t.id"
          type="button"
          class="chat-sessions-archived-item"
          @click="s.unarchiveThread(t.id)"
        >{{ t.name }}</button>
      </div>
    </div>
  </aside>
</template>

<style scoped>
.chat-sessions {
  width: 240px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid var(--rule);
  background: var(--bg-card);
}

.chat-sessions-head {
  padding: 12px 14px 6px;
}

.chat-sessions-title {
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--ink-3);
}

.chat-session-new {
  margin: 0 10px 8px;
  padding: 8px 10px;
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  background: transparent;
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  color: var(--ink-2);
  cursor: pointer;
  text-align: left;
}

.chat-session-new:hover {
  background: var(--bg-hover);
  color: var(--ink);
  border-color: var(--accent);
}

.chat-session-new-icon {
  font-size: 15px;
  line-height: 1;
}

.chat-session-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 0 8px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.chat-session-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 7px 8px;
  border-radius: var(--r-sm);
  font-size: 13px;
  color: var(--ink-2);
  cursor: pointer;
}

.chat-session-row:hover {
  background: var(--bg-hover);
}

.chat-session-row--active {
  background: var(--bg-active);
  color: var(--ink);
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

.chat-session-actions {
  display: none;
  gap: 2px;
}

.chat-session-row:hover .chat-session-actions {
  display: inline-flex;
}

.chat-session-btn {
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
  padding: 6px 8px;
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

.chat-sessions-archived-item {
  display: block;
  width: 100%;
  padding: 6px 10px;
  font-size: 12px;
  text-align: left;
  background: transparent;
  border: none;
  color: var(--ink-2);
  cursor: pointer;
  border-radius: var(--r-sm);
}

.chat-sessions-archived-item:hover {
  background: var(--bg-hover);
  color: var(--ink);
}
</style>
