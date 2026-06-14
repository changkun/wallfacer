<script setup lang="ts">
import { computed, onMounted } from 'vue';
import { RouterView, useRoute, useRouter } from 'vue-router';
import AppLayout from './layouts/AppLayout.vue';
import WorkspaceRequired from './components/WorkspaceRequired.vue';
import { hashToRoute } from './lib/hashRoute';
import { useTaskStore } from './stores/tasks';

const router = useRouter();
const route = useRoute();
const store = useTaskStore();

// A workspace is "available" only when config has loaded and reports at least
// one workspace visible to this session. Workspace-scoped routes fall back to
// the WorkspaceRequired prompt otherwise, matching the board's empty state.
const hasWorkspace = computed(() => (store.config?.workspaces?.length ?? 0) > 0);
const blockForWorkspace = computed(
  () => route.meta?.needsWorkspace === true && store.config != null && !hasWorkspace.value,
);

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
    <WorkspaceRequired v-if="blockForWorkspace" />
    <RouterView v-else :connected="connected" :conn-state="connState" />
  </AppLayout>
  <RouterView v-else />
</template>
