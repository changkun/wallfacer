<script setup lang="ts">
import { useBootStore } from '../stores/boot';

const boot = useBootStore();
</script>

<template>
  <div class="board-placeholder">
    <div class="board-placeholder-inner">
      <h1>Wallfacer</h1>
      <p>Task board coming soon.</p>
      <p class="board-placeholder-meta">
        Mode: {{ boot.mode }} &middot; Version: {{ boot.version || 'dev' }}
      </p>
    </div>
  </div>
</template>

<style scoped>
.board-placeholder {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  background: var(--bg, #f4f1ea);
  color: var(--text, #1b1916);
  font-family: var(--font-sans, system-ui, sans-serif);
}
.board-placeholder-inner {
  text-align: center;
}
.board-placeholder-inner h1 {
  font-size: 2rem;
  margin-bottom: 0.5rem;
}
.board-placeholder-meta {
  margin-top: 1rem;
  font-size: 0.85rem;
  opacity: 0.5;
}
</style>
