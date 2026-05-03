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

function clearQuery() {
  query.value = '';
}
</script>

<template>
  <div class="task-search-wrapper app-header__search">
    <span class="task-search-icon">
      <svg
        width="14"
        height="14"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2.5"
        stroke-linecap="round"
        stroke-linejoin="round"
      >
        <circle cx="11" cy="11" r="8"></circle>
        <line x1="21" y1="21" x2="16.65" y2="16.65"></line>
      </svg>
    </span>
    <input
      v-model="query"
      type="search"
      class="task-search-input"
      placeholder="Filter tasks… or @search server"
      autocomplete="off"
      @keydown="onKeydown"
    />
    <button
      v-show="query.length > 0"
      type="button"
      class="task-search-clear"
      style="display: block"
      title="Clear search"
      @click="clearQuery"
    >
      &times;
    </button>
  </div>
</template>
