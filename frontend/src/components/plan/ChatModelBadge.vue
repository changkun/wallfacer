<script setup lang="ts">
// ChatModelBadge — the session-primary model chip ("Claude · Opus 4.8") shown
// in a chat surface's header. Sourced from the model the harness reports in its
// init line; renders nothing until one has been observed. The raw model id
// (with any context-variant suffix) is preserved on hover. Shared by the three
// chat surfaces (ChatPage, AgentChatPanel, SpecChatPopup) so they stay in
// lockstep. The chat runtime is Claude-only today, so the harness is fixed.
import { computed } from 'vue';
import HarnessBadge from '../HarnessBadge.vue';
import { modelLabel } from '../../lib/harness';

const props = defineProps<{ model: string }>();
const label = computed(() => modelLabel(props.model));
</script>

<template>
  <span v-if="label" class="chat-model-badge" :title="model">
    <HarnessBadge harness="claude" :size="14" />
    <span class="chat-model-sep" aria-hidden="true">·</span>
    <span class="chat-model-name">{{ label }}</span>
  </span>
</template>

<style scoped>
.chat-model-badge {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  font-size: 11px;
  color: var(--ink-4);
  cursor: default;
  white-space: nowrap;
}
.chat-model-sep { color: var(--ink-4); }
.chat-model-name { font-weight: 500; color: var(--ink-3); }
</style>
