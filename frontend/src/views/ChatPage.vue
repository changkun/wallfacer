<script setup lang="ts">
// ChatPage — the dedicated Claude-style chat surface (/chat). A session
// sub-sidebar plus a conversation that starts from a centered entry screen
// (greeting + hero composer + quick actions) and morphs into the message
// stream on the first send. Supersedes chat-first mode as the single empty
// state. All conversation behaviour comes from the shared chat core.
import { ref, computed } from 'vue';
import { useChatSession } from '../composables/useChatSession';
import SessionList from '../components/plan/SessionList.vue';
import ChatMessageList from '../components/plan/ChatMessageList.vue';
import ChatComposer from '../components/plan/ChatComposer.vue';

const chat = useChatSession();

// Entry screen while the active session has no messages; the first user bubble
// flips this to the conversation view, producing the centered→docked morph.
const showEntry = computed(() => chat.renderedMessages.value.length === 0);

const heroComposer = ref<{ setText: (t: string) => void } | null>(null);

const QUICK_ACTIONS = [
  { label: 'Draft a spec', icon: '✎', insert: '/create ' },
  { label: 'Break down', icon: '⊞', insert: '/break-down ' },
  { label: 'Dispatch', icon: '↗', insert: '/dispatch' },
];

function applyQuick(insert: string) {
  heroComposer.value?.setText(insert);
}
</script>

<template>
  <div class="chat-page">
    <SessionList :session="chat" />

    <div class="chat-main">
      <Transition name="chat-morph" mode="out-in">
        <!-- Entry screen -->
        <div v-if="showEntry" key="entry" class="chat-entry">
          <div class="chat-entry-inner">
            <h1 class="chat-entry-greeting">
              <span class="chat-entry-mark" aria-hidden="true">✳</span>
              What should we plan?
            </h1>
            <ChatComposer
              ref="heroComposer"
              :streaming="chat.streaming.value"
              variant="hero"
              placeholder="Message the planning agent…"
              @send="chat.sendMessage"
              @interrupt="chat.onInterrupt"
            />
            <div class="chat-entry-quick">
              <button
                v-for="q in QUICK_ACTIONS"
                :key="q.label"
                type="button"
                class="chat-entry-chip"
                @click="applyQuick(q.insert)"
              >
                <span class="chat-entry-chip-icon">{{ q.icon }}</span> {{ q.label }}
              </button>
            </div>
          </div>
        </div>

        <!-- Conversation -->
        <div v-else key="conversation" class="chat-conversation">
          <header class="chat-conversation-head">
            <span class="chat-conversation-title">Chat</span>
            <button
              type="button"
              class="chat-conversation-clear"
              title="Clear conversation"
              @click="chat.clearHistory"
            >Clear</button>
          </header>
          <ChatMessageList :session="chat" />
          <div class="chat-conversation-composer">
            <ChatComposer
              :streaming="chat.streaming.value"
              variant="docked"
              placeholder="Message the planning agent…"
              @send="chat.sendMessage"
              @interrupt="chat.onInterrupt"
            />
          </div>
        </div>
      </Transition>
    </div>
  </div>
</template>

<style scoped>
.chat-page {
  display: flex;
  height: 100%;
  overflow: hidden;
  background: var(--bg);
  color: var(--ink);
  font-family: var(--font-sans);
}

.chat-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

/* ── Entry screen ──────────────────────────────────────────────────── */

.chat-entry {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}

.chat-entry-inner {
  width: 100%;
  max-width: 680px;
}

.chat-entry-greeting {
  text-align: center;
  font-size: 30px;
  font-weight: 500;
  letter-spacing: -0.01em;
  color: var(--ink);
  margin: 0 0 24px;
}

.chat-entry-mark {
  color: var(--accent);
  margin-right: 6px;
}

.chat-entry-quick {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  justify-content: center;
  margin-top: 16px;
}

.chat-entry-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 14px;
  font-size: 13px;
  background: var(--bg-card);
  border: 1px solid var(--rule);
  border-radius: 999px;
  color: var(--ink-2);
  cursor: pointer;
}

.chat-entry-chip:hover {
  background: var(--bg-hover);
  color: var(--ink);
  border-color: var(--accent);
}

.chat-entry-chip-icon {
  color: var(--accent);
}

/* ── Conversation ──────────────────────────────────────────────────── */

.chat-conversation {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.chat-conversation-head {
  padding: 10px 16px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-bottom: 1px solid var(--rule);
}

.chat-conversation-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--ink);
}

.chat-conversation-clear {
  font-size: 11px;
  padding: 2px 8px;
  background: transparent;
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  color: var(--ink-3);
  cursor: pointer;
}

.chat-conversation-clear:hover {
  background: var(--bg-hover);
  color: var(--ink);
}

.chat-conversation :deep(.pcp-stream) {
  max-width: 820px;
  width: 100%;
  margin: 0 auto;
}

.chat-conversation-composer {
  padding: 8px 16px 16px;
  max-width: 820px;
  width: 100%;
  margin: 0 auto;
  box-sizing: border-box;
}

/* ── Morph transition ──────────────────────────────────────────────── */

.chat-morph-enter-active,
.chat-morph-leave-active {
  transition: opacity 180ms ease, transform 240ms cubic-bezier(0.2, 0, 0, 1);
}

.chat-morph-enter-from {
  opacity: 0;
  transform: translateY(8px);
}

.chat-morph-leave-to {
  opacity: 0;
  transform: translateY(-8px);
}

@media (prefers-reduced-motion: reduce) {
  .chat-morph-enter-active,
  .chat-morph-leave-active {
    transition: none;
  }
  .chat-morph-enter-from,
  .chat-morph-leave-to {
    transform: none;
  }
}
</style>
