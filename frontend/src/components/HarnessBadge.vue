<script setup lang="ts">
// A harness chip: monochrome logo + brand-cased label. Used wherever a
// harness is surfaced as a label (settings blocks, task harness display,
// dropdown rows).
import { computed } from 'vue';
import HarnessLogo from './HarnessLogo.vue';
import { harnessLabel } from '../lib/harness';

const props = withDefaults(
  defineProps<{ harness: string; size?: number | string; label?: string }>(),
  { size: 16 },
);

const text = computed(() => props.label ?? harnessLabel(props.harness));
</script>

<template>
  <span class="harness-badge">
    <HarnessLogo :harness="harness" :size="size" />
    <span class="harness-badge__label">{{ text }}</span>
  </span>
</template>

<style scoped>
.harness-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  white-space: nowrap;
}
.harness-badge__label {
  font-weight: 600;
}
</style>
