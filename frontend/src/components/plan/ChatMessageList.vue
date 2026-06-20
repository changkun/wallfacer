<script setup lang="ts">
// ChatMessageList — the scrollable conversation. A signature wallfacer look,
// not a Claude clone: assistant turns are led by the pixel brand mark and flow
// as full-width prose; user turns are right-aligned high-contrast pills. Pure
// presentation over a ChatSession; mounted by the Chat view, the docked panel,
// and the spec-mode popup alike.
import { activityIcon, activitySummary } from '../../lib/planningBubble';
import BrandMark from '../BrandMark.vue';
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

        <!-- User turn: right-aligned high-contrast pill -->
        <div v-else-if="m.role === 'user'" class="pcp-turn pcp-turn--user">
          <div v-if="m.errorText" class="pcp-bubble-error">{{ m.errorText }}</div>
          <div class="pcp-usermsg">{{ m.rawText }}</div>
        </div>

        <!-- Assistant turn: pixel-mark signature + full-width prose -->
        <div v-else class="pcp-turn pcp-turn--assistant" :class="{ 'pcp-turn--reverted': m.reverted }">
          <div class="pcp-agent-mark">
            <BrandMark :size="15" />
          </div>
          <div class="pcp-agent-body">
            <div v-if="m.errorText" class="pcp-bubble-error">{{ m.errorText }}</div>
            <!-- Trajectory leads the answer: live & open while streaming, then
                 collapsed into an informative one-liner above the prose. -->
            <details v-if="m.hasActivity" class="pcp-activity" :open="m.isStreaming">
              <summary>
                <span class="pcp-activity-title">{{
                  m.isStreaming ? 'Working…' : activitySummary(m.activity)
                }}</span>
              </summary>
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
              v-if="m.contentHtml"
              class="pcp-bubble-content prose-content"
              v-html="m.contentHtml"
            />
            <div v-else-if="m.isStreaming" class="pcp-thinking"><i></i><i></i><i></i></div>
            <div v-if="m.planRound > 0 && !m.reverted" class="pcp-turn-actions">
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
