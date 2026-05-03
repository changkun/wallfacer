<script setup lang="ts">
import { ref, watch } from 'vue';
import { useTaskStore } from '../stores/tasks';

const store = useTaskStore();
const query = ref('');

watch(query, (q) => {
  store.filterQuery = q.trim().toLowerCase();
});

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    query.value = '';
    (e.target as HTMLInputElement).blur();
  }
}
</script>

<template>
  <div class="search-bar">
    <input
      v-model="query"
      class="search-input"
      type="text"
      placeholder="Filter tasks... or @search"
      @keydown="onKeydown"
    />
  </div>
</template>

<style scoped>
.search-bar {
  padding: 0 var(--sp-5);
  display: flex;
  align-items: center;
  height: var(--h-header);
  border-bottom: 1px solid var(--rule);
  flex-shrink: 0;
}
.search-input {
  width: 100%;
  max-width: 400px;
  border: 1px solid var(--rule);
  border-radius: var(--r-sm);
  background: var(--bg-sunk);
  color: var(--ink);
  font-family: var(--font-sans);
  font-size: 12px;
  padding: 4px 10px;
  outline: none;
}
.search-input:focus { border-color: var(--accent); }
.search-input::placeholder { color: var(--ink-4); }
</style>
