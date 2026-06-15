<script setup lang="ts">
// BrandMark — the wallfacer pixel logo, sized on demand. Used as the planning
// agent's signature in chat turns and on the chat entry screen, so the brand
// mark (not a generic avatar or another product's glyph) carries identity.
withDefaults(defineProps<{ size?: number }>(), { size: 16 });
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 16 16"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    style="display:block;image-rendering:pixelated"
    aria-hidden="true"
  >
    <rect x="0" y="0" width="6" height="3" fill="var(--accent)" />
    <rect x="7" y="0" width="9" height="3" fill="var(--accent-2)" />
    <rect x="0" y="4" width="4" height="3" fill="#8a3e21" />
    <rect x="5" y="4" width="6" height="3" fill="var(--accent)" />
    <rect x="12" y="4" width="4" height="3" fill="var(--accent-2)" />
    <rect x="0" y="8" width="7" height="3" fill="var(--accent-2)" />
    <rect x="8" y="8" width="8" height="3" fill="#8a3e21" />
    <rect x="0" y="12" width="3" height="4" fill="var(--accent)" />
    <rect x="4" y="12" width="6" height="4" fill="#8a3e21" />
    <rect x="11" y="12" width="5" height="4" fill="var(--accent)" />
  </svg>
</template>
