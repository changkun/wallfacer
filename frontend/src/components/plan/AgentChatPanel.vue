<script setup lang="ts">
// AgentChatPanel — the docked spec-mode chat column. Panel chrome (header +
// thread tabs) only; the conversation stream, composer, and all session logic
// come from the shared chat core (useChatSession + ChatMessageList +
// ChatComposer) so this surface stays in lockstep with the dedicated Chat view
// and the floating popup.
import { storeToRefs } from 'pinia';
import { useAgentStore } from '../../stores/agentSession';
import { useChatSession } from '../../composables/useChatSession';
import ChatMessageList from './ChatMessageList.vue';
import ChatComposer from './ChatComposer.vue';

defineProps<{ visible: boolean }>();
const emit = defineEmits<{ toggle: [] }>();

const planning = useAgentStore();
const { threads, threadOrder, archivedThreads, activeThreadId } = storeToRefs(planning);

const chat = useChatSession();

defineExpose({
  send(text: string) {
    void chat.sendMessage(text);
  },
});
</script>

<template>
  <aside v-show="visible" class="agent-chat-panel">
    <header class="pcp-header">
      <span class="pcp-title">Chat</span>
      <div class="pcp-header-actions">
        <button
          type="button"
          class="pcp-clear"
          title="Clear conversation"
          @click="chat.clearHistory"
        >Clear</button>
        <button
          type="button"
          class="pcp-fold"
          title="Hide chat (C)"
          @click="emit('toggle')"
        >✕</button>
      </div>
    </header>

    <div class="pcp-tabs" role="tablist" aria-label="Chat sessions">
      <template v-for="id in threadOrder" :key="id">
        <component
          :is="id === activeThreadId ? 'div' : 'button'"
          class="pcp-tab"
          :class="{ 'pcp-tab--active': id === activeThreadId }"
          :role="id === activeThreadId ? 'tab' : undefined"
          :type="id === activeThreadId ? undefined : 'button'"
          @click="id === activeThreadId ? null : chat.switchToThread(id)"
        >
          <input
            v-if="chat.renamingId.value === id"
            v-model="chat.renameDraft.value"
            class="pcp-tab-rename"
            type="text"
            @keydown.enter.prevent="chat.commitRename"
            @keydown.escape.prevent="chat.cancelRename"
            @blur="chat.commitRename"
            @click.stop
          />
          <template v-else>
            <span class="pcp-tab-label">{{ threads[id]?.name }}</span>
            <span v-if="id !== activeThreadId && threads[id]?.unread" class="pcp-tab-unread" />
            <button
              v-if="id === activeThreadId"
              type="button"
              class="pcp-tab-pencil"
              title="Rename"
              @click.stop="chat.startRename(id)"
            >✎</button>
            <button
              type="button"
              class="pcp-tab-close"
              title="Archive thread"
              @click.stop="chat.archiveThread(id)"
            >×</button>
          </template>
        </component>
      </template>
      <div
        v-if="chat.draft.value"
        class="pcp-tab pcp-tab--active"
        role="tab"
      >
        <span class="pcp-tab-label">New chat</span>
      </div>
      <span class="pcp-tab-newwrap">
        <button
          type="button"
          class="pcp-tab-new"
          title="New thread"
          @click="chat.createThread"
        >+</button>
        <button
          v-if="archivedThreads.length > 0"
          type="button"
          class="pcp-tab-archive-trigger"
          title="Archived threads"
          @click="chat.archiveMenuOpen.value = !chat.archiveMenuOpen.value"
        >▾</button>
        <div v-if="chat.archiveMenuOpen.value" class="pcp-archived-menu">
          <div class="pcp-archived-menu-header">Archived threads</div>
          <button
            v-for="t in archivedThreads"
            :key="t.id"
            type="button"
            class="pcp-archived-menu-item"
            @click="chat.unarchiveThread(t.id)"
          >{{ t.name }}</button>
        </div>
      </span>
    </div>

    <ChatMessageList :session="chat" />

    <ChatComposer
      :streaming="chat.streaming.value"
      variant="panel"
      @send="chat.sendMessage"
      @interrupt="chat.onInterrupt"
    />
  </aside>
</template>

<style scoped src="./AgentChatPanel.css"></style>
