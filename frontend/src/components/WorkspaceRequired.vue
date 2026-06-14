<script setup lang="ts">
// Shown in place of any workspace-scoped view (board, plan, agents, flows,
// routines, analytics, map) when no workspace is visible to the current
// session. Keeps every surface consistent with /api/config's "no workspace"
// state instead of rendering an empty or stale workspace-scoped UI.
import { useUiStore } from '../stores/ui';

const ui = useUiStore();
</script>

<template>
  <main class="ws-required">
    <div class="ws-required__inner">
      <h1 class="ws-required__title">Pick a workspace to begin</h1>
      <p class="ws-required__hint">
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

<style scoped>
/* Self-contained so the prompt renders correctly outside the board, where
   BoardPage's scoped .board-empty styles do not reach. */
.ws-required {
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--sp-5);
}
.ws-required__inner {
  width: min(560px, 100%);
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 12px;
  text-align: center;
}
.ws-required__title {
  font-family: var(--font-serif);
  font-style: italic;
  font-weight: 400;
  font-size: var(--fs-3xl, 28px);
  color: var(--ink);
  margin: 0;
}
.ws-required__hint {
  margin: 0 0 4px;
  color: var(--ink-3);
  font-size: var(--fs-md);
}
</style>
