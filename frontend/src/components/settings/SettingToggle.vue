<script setup lang="ts">
// A two-state control for a setting: the segmented primitive with Off / On.
// Reads as a switch (role="switch"), so assistive tech announces the state.
defineProps<{ modelValue: boolean; disabled?: boolean; label?: string }>();
const emit = defineEmits<{ 'update:modelValue': [boolean] }>();
</script>

<template>
  <button
    type="button"
    class="seg compact setting-toggle"
    role="switch"
    :aria-checked="modelValue"
    :aria-label="label"
    :disabled="disabled"
    @click="emit('update:modelValue', !modelValue)"
  >
    <span class="seg-btn" :class="{ on: !modelValue }">Off</span>
    <span class="seg-btn" :class="{ on: modelValue }">On</span>
  </button>
</template>

<style scoped>
.setting-toggle {
  cursor: pointer;
  border: 1px solid var(--rule);
}
.setting-toggle:disabled {
  opacity: 0.5;
  cursor: progress;
}
.setting-toggle .seg-btn {
  pointer-events: none;
}
.setting-toggle .seg-btn.on:last-child {
  color: var(--accent);
}
</style>
