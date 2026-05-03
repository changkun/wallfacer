<script setup lang="ts">
import { ref, onMounted, markRaw, type Component } from 'vue';
import ProductPage from './ProductPage.vue';

const page = ref<Component>(ProductPage);

onMounted(async () => {
  if (window.__WALLFACER__?.mode === 'local') {
    const mod = await import('./BoardPage.vue');
    page.value = markRaw(mod.default);
  }
});
</script>

<template>
  <component :is="page" />
</template>
