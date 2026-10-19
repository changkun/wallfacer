<script setup lang="ts">
import { RouterView } from 'vue-router';
import AppLayout from './layouts/AppLayout.vue';
import WorkspaceRequired from './components/WorkspaceRequired.vue';
import { useWorkspaceGate } from './composables/useWorkspaceGate';

// Workspace-scoped routes fall back to the WorkspaceRequired prompt when no
// workspace is visible to this session, matching the board's empty state.
const blockForWorkspace = useWorkspaceGate();
</script>

<template>
  <AppLayout v-slot="{ connected, connState }">
    <WorkspaceRequired v-if="blockForWorkspace" />
    <RouterView v-else :connected="connected" :conn-state="connState" />
  </AppLayout>
</template>
