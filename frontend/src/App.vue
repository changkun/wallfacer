<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { RouterView, useRouter } from 'vue-router';
import AppLayout from './layouts/AppLayout.vue';
import { hashToRoute } from './lib/hashRoute';

const router = useRouter();

const isLocal = computed(() => {
  if (typeof window !== 'undefined' && window.__WALLFACER__) {
    return window.__WALLFACER__.mode === 'local';
  }
  return false;
});

// Migrate legacy hash-mode bookmarks (#<uuid>, #plan/<path>) to history routes.
onMounted(() => {
  if (typeof window === 'undefined' || !window.location.hash) return;
  const target = hashToRoute(window.location.hash);
  if (!target) return;
  history.replaceState(null, '', window.location.pathname + window.location.search);
  void router.replace(target);
});
</script>

<template>
  <AppLayout v-if="isLocal" v-slot="{ connected, connState }">
    <RouterView :connected="connected" :conn-state="connState" />
  </AppLayout>
  <RouterView v-else />
</template>
