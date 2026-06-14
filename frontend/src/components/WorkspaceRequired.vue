<script setup lang="ts">
// Shown in place of any workspace-scoped view (board, plan, agents, flows,
// routines, analytics, map) when no workspace is visible to the current
// session. Keeps every surface consistent with /api/config's "no workspace"
// state instead of rendering an empty or stale workspace-scoped UI.
import { useUiStore } from '../stores/ui';

const ui = useUiStore();
</script>

<template>
  <main class="board-empty">
    <div class="board-empty__inner">
      <h1 class="board-empty__title">Pick a workspace to begin</h1>
      <p class="board-empty__hint">
        Wallfacer scopes every task to a workspace directory. Choose
        one (or more) before creating tasks.
      </p>
      <button
        type="button"
        class="composer__btn composer__btn--primary"
        @click="ui.showWorkspaces = true"
      >Open workspace picker</button>
    </div>
  </main>
</template>
