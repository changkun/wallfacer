<script setup lang="ts">
// ChatPage — the dedicated Claude-style chat surface (/chat). A session
// sub-sidebar plus a conversation that starts from a centered entry screen
// (greeting + hero composer + quick actions) and morphs into the message
// stream on the first send. Supersedes chat-first mode as the single empty
// state. All conversation behaviour comes from the shared chat core.
import { ref, computed } from 'vue';
import { useChatSession } from '../composables/useChatSession';
import { aggregateUsage, formatTokens, formatCost, formatPercent } from '../lib/agentUsage';
import BrandMark from '../components/BrandMark.vue';
import SessionList from '../components/plan/SessionList.vue';
import ChatMessageList from '../components/plan/ChatMessageList.vue';
import ChatComposer from '../components/plan/ChatComposer.vue';

const chat = useChatSession();

// Entry screen while the active session has no messages; the first user bubble
// flips this to the conversation view, producing the centered→docked morph.
const showEntry = computed(() => chat.renderedMessages.value.length === 0);

// Per-thread token/cost rollup for the header, summed from each turn's usage.
const usage = computed(() => aggregateUsage(chat.renderedMessages.value));
const usageTooltip = computed(() => {
  const u = usage.value;
  return [
    `${u.rounds} assistant ${u.rounds === 1 ? 'round' : 'rounds'}`,
    `Input: ${u.inputTokens.toLocaleString()} fresh + ${u.cacheReadTokens.toLocaleString()} from cache`,
    `Output: ${u.outputTokens.toLocaleString()} (includes reasoning)`,
    u.cacheCreationTokens ? `Cache writes: ${u.cacheCreationTokens.toLocaleString()}` : '',
    `Cache hit: ${formatPercent(u.cacheHitRatio)} of input served from cache`,
    `Cost: ${formatCost(u.costUSD)}`,
  ].filter(Boolean).join('\n');
});

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
            <div class="chat-entry-mark" aria-hidden="true">
              <BrandMark :size="34" />
            </div>
            <h1 class="chat-entry-greeting">What should we work on?</h1>
            <ChatComposer
              ref="heroComposer"
              :streaming="chat.streaming.value"
              variant="hero"
              placeholder="Message the agent…"
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
            <div v-if="usage.rounds > 0" class="chat-usage" :title="usageTooltip">
              <span class="chat-usage-item">{{ usage.rounds }} {{ usage.rounds === 1 ? 'round' : 'rounds' }}</span>
              <span class="chat-usage-item">↑ {{ formatTokens(usage.inputTokens) }}</span>
              <span class="chat-usage-item">↓ {{ formatTokens(usage.outputTokens) }}</span>
              <span v-if="usage.cacheReadTokens" class="chat-usage-item chat-usage-cache">♻ {{ formatPercent(usage.cacheHitRatio) }} cached</span>
              <span class="chat-usage-item chat-usage-cost">{{ formatCost(usage.costUSD) }}</span>
            </div>
          </header>
          <ChatMessageList :session="chat" />
          <div class="chat-conversation-composer">
            <ChatComposer
              :streaming="chat.streaming.value"
              variant="docked"
              placeholder="Message the agent…"
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

.chat-entry-inner {
  position: relative;
}

/* Soft ember glow behind the entry, the signature "shine". */
.chat-entry-inner::before {
  content: '';
  position: absolute;
  left: 50%;
  top: -40px;
  width: 460px;
  height: 320px;
  transform: translateX(-50%);
  background: radial-gradient(
    ellipse at center,
    color-mix(in oklab, var(--accent) 16%, transparent),
    transparent 70%
  );
  filter: blur(8px);
  pointer-events: none;
  z-index: 0;
}

.chat-entry-mark {
  position: relative;
  z-index: 1;
  display: flex;
  justify-content: center;
  margin-bottom: 16px;
  filter: drop-shadow(0 4px 12px color-mix(in oklab, var(--accent) 35%, transparent));
}

.chat-entry-greeting {
  position: relative;
  z-index: 1;
  text-align: center;
  font-family: var(--font-serif);
  font-style: italic;
  font-size: 42px;
  font-weight: 400;
  letter-spacing: 0.01em;
  color: var(--ink);
  margin: 0 0 26px;
}

.chat-entry-inner :deep(.pcp-composer) {
  position: relative;
  z-index: 1;
}

.chat-entry-quick {
  position: relative;
  z-index: 1;
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

.chat-usage {
  display: flex;
  gap: 10px;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  color: var(--ink-4);
  cursor: default;
  white-space: nowrap;
  overflow: hidden;
}
.chat-usage-cache { color: var(--ok); }
.chat-usage-cost { color: var(--ink-3); font-weight: 500; }

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
