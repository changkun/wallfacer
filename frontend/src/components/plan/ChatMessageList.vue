<script setup lang="ts">
// ChatMessageList — the scrollable conversation, rendered as Claude-style turns:
// user messages as right-aligned bubbles, assistant replies as full-width prose
// (no avatars, no dev-log meta rows). Pure presentation over a ChatSession;
// mounted identically by the dedicated Chat view, the docked panel, and the
// spec-mode popup.
import { activityIcon } from '../../lib/planningBubble';
import type { ChatSession } from '../../composables/useChatSession';

const props = defineProps<{ session: ChatSession; emptyText?: string }>();
const s = props.session;
</script>

<template>
  <div class="pcp-stream">
    <div
      :ref="el => (s.messagesEl.value = el as HTMLElement | null)"
      class="pcp-messages"
    >
      <div v-if="s.renderedMessages.value.length === 0" class="pcp-empty">
        {{ emptyText ?? 'No messages yet.' }}
      </div>
      <template v-for="(m, i) in s.renderedMessages.value" :key="i">
        <div v-if="m.role === 'system'" class="pcp-system">{{ m.rawText }}</div>

        <!-- User turn: right-aligned bubble -->
        <div v-else-if="m.role === 'user'" class="pcp-turn pcp-turn--user">
          <div v-if="m.errorText" class="pcp-bubble-error">{{ m.errorText }}</div>
          <div class="pcp-usermsg">{{ m.rawText }}</div>
        </div>

        <!-- Assistant turn: full-width prose -->
        <div v-else class="pcp-turn pcp-turn--assistant" :class="{ 'pcp-turn--reverted': m.reverted }">
          <div v-if="m.errorText" class="pcp-bubble-error">{{ m.errorText }}</div>
          <div
            v-if="m.contentHtml"
            class="pcp-bubble-content prose-content"
            v-html="m.contentHtml"
          />
          <div v-else-if="m.isStreaming" class="pcp-thinking"><span>.</span><span>.</span><span>.</span></div>
          <details v-if="m.hasActivity" class="pcp-activity" :open="m.isStreaming">
            <summary>Agent activity</summary>
            <div class="pcp-activity-log">
              <div
                v-for="(row, ri) in m.activity"
                :key="ri"
                class="pcp-activity-row"
                :class="'pcp-activity-row--' + row.kind"
              >
                <span class="pcp-activity-icon">{{ activityIcon(row.kind) }}</span>
                <span class="pcp-activity-label">{{ row.label }}</span>
                <span v-if="row.summary" class="pcp-activity-summary">{{ row.summary }}</span>
                <details v-if="row.detail" class="pcp-activity-detail" :open="row.defaultOpen">
                  <summary>show</summary>
                  <pre>{{ row.detail }}</pre>
                </details>
              </div>
            </div>
          </details>
          <div
            v-if="m.planRound > 0 && !m.reverted"
            class="pcp-turn-actions"
          >
            <button
              type="button"
              class="pcp-undo"
              :disabled="m.planRound !== s.latestRound.value"
              :title="m.planRound === s.latestRound.value ? 'Undo round ' + m.planRound : 'Only the most recent round can be undone'"
              :aria-label="'Undo round ' + m.planRound"
              @click="s.undoRound(m)"
            >↺ undo</button>
          </div>
        </div>

        <div v-if="i === s.interruptedAt.value" class="pcp-interrupted">interrupted</div>
      </template>
    </div>

    <div class="pcp-queue">
      <div v-for="q in s.currentQueue.value" :key="q.id" class="pcp-queue-chip">
        <input
          v-if="s.editingQueueId.value === q.id"
          v-model="s.editQueueDraft.value"
          class="pcp-queue-edit"
          @keydown.enter.prevent="s.commitQueueEdit(q.id)"
          @keydown.esc.prevent="s.cancelQueueEdit"
          @blur="s.commitQueueEdit(q.id)"
        />
        <span
          v-else
          class="pcp-queue-text"
          title="Double-click to edit"
          @dblclick="s.startQueueEdit(q)"
        >{{ q.text }}</span>
        <button
          type="button"
          class="pcp-queue-remove"
          @click="s.removeFromQueue(q.id)"
        >×</button>
      </div>
    </div>
  </div>
</template>

<style scoped src="./ChatMessageList.css"></style>
