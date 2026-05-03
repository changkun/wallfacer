<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue';

const ProductPage = defineAsyncComponent(() => import('./ProductPage.vue'));
const BoardPage = defineAsyncComponent(() => import('./BoardPage.vue'));

const isLocal = computed(() => {
  if (typeof window !== 'undefined' && window.__WALLFACER__) {
    return window.__WALLFACER__.mode === 'local';
  }
  return false;
});
</script>

<template>
  <BoardPage v-if="isLocal" />
  <ProductPage v-else />
</template>
