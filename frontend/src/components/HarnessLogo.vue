<script setup lang="ts">
// Monochrome harness marks. Each renders in currentColor so it adapts to
// light/dark themes and inherits the surrounding text color. The marks are
// simplified, recognizable glyphs (not pixel-exact trademarks) sized to a
// 24x24 viewBox.
import { computed } from 'vue';

const props = withDefaults(defineProps<{ harness: string; size?: number | string }>(), {
  size: 16,
});

const id = computed(() => (props.harness || '').toLowerCase());
const px = computed(() => (typeof props.size === 'number' ? `${props.size}px` : props.size));
</script>

<template>
  <svg
    :width="px"
    :height="px"
    viewBox="0 0 24 24"
    class="harness-logo"
    role="img"
    :aria-label="`${id} logo`"
    fill="none"
  >
    <!-- Claude / Anthropic: radiating sunburst -->
    <g v-if="id === 'claude'" stroke="currentColor" stroke-width="1.6" stroke-linecap="round">
      <line x1="12" y1="3" x2="12" y2="9" />
      <line x1="12" y1="15" x2="12" y2="21" />
      <line x1="3" y1="12" x2="9" y2="12" />
      <line x1="15" y1="12" x2="21" y2="12" />
      <line x1="5.6" y1="5.6" x2="9.2" y2="9.2" />
      <line x1="14.8" y1="14.8" x2="18.4" y2="18.4" />
      <line x1="18.4" y1="5.6" x2="14.8" y2="9.2" />
      <line x1="9.2" y1="14.8" x2="5.6" y2="18.4" />
    </g>

    <!-- Codex / OpenAI: six-fold rosette -->
    <g v-else-if="id === 'codex'" stroke="currentColor" stroke-width="1.4">
      <ellipse cx="12" cy="12" rx="3" ry="8.5" />
      <ellipse cx="12" cy="12" rx="3" ry="8.5" transform="rotate(60 12 12)" />
      <ellipse cx="12" cy="12" rx="3" ry="8.5" transform="rotate(120 12 12)" />
    </g>

    <!-- Cursor: pointer arrow -->
    <path
      v-else-if="id === 'cursor'"
      d="M5 3.2 L5 17.6 L8.7 14.2 L11.2 20 L13.4 19 L10.9 13.3 L16.2 13.3 Z"
      fill="currentColor"
    />

    <!-- OpenCode: terminal prompt -->
    <g v-else-if="id === 'opencode'" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
      <polyline points="6 8 10 12 6 16" />
      <line x1="12" y1="16" x2="18" y2="16" />
    </g>

    <!-- Pi: greek letter -->
    <g v-else-if="id === 'pi'" stroke="currentColor" stroke-width="1.9" stroke-linecap="round">
      <line x1="4.5" y1="7.5" x2="19.5" y2="7.5" />
      <line x1="9" y1="7.5" x2="8.2" y2="17.5" />
      <line x1="15" y1="7.5" x2="15" y2="17.5" />
    </g>

    <!-- Fallback: generic agent dot -->
    <circle v-else cx="12" cy="12" r="5" stroke="currentColor" stroke-width="1.6" />
  </svg>
</template>

<style scoped>
.harness-logo {
  display: inline-block;
  vertical-align: middle;
  flex: none;
}
</style>
