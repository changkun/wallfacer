<script setup lang="ts">
import { computed } from 'vue';
import { RouterView } from 'vue-router';
import AppLayout from './layouts/AppLayout.vue';

const isLocal = computed(() => {
  if (typeof window !== 'undefined' && window.__WALLFACER__) {
    return window.__WALLFACER__.mode === 'local';
  }
  return false;
});
</script>

<template>
  <AppLayout v-if="isLocal" v-slot="{ connected }">
    <RouterView :connected="connected" />
  </AppLayout>
  <RouterView v-else />
</template>
