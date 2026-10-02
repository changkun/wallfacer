<script setup lang="ts">
// The console's not-found page: the local router's catch-all renders it for
// any address no console page serves, inside the console shell so the rail
// stays available. The cloud build has its own NotFoundPage with the site
// layout.
import { RouterLink } from 'vue-router';
</script>

<template>
  <main class="not-found">
    <div class="not-found__inner">
      <span class="eyebrow">404</span>
      <h1 class="not-found__title">Page not found</h1>
      <p class="not-found__hint">No console page lives at this address.</p>
      <RouterLink to="/" class="btn not-found__home">Go to the board</RouterLink>
    </div>
  </main>
</template>

<style scoped>
.not-found {
  flex: 1;
  min-height: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: var(--sp-5);
}
.not-found__inner {
  width: min(560px, 100%);
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  text-align: center;
}
.not-found__title {
  font-family: var(--font-display);
  font-weight: 600;
  font-size: var(--fs-3xl, 28px);
  color: var(--ink);
  margin: 0;
}
.not-found__hint {
  margin: 0 0 4px;
  color: var(--ink-3);
  font-size: var(--fs-md);
}
</style>
