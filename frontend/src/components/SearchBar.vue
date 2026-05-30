<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted } from 'vue';
import { useTaskStore } from '../stores/tasks';
import { useUiStore } from '../stores/ui';

const store = useTaskStore();
const ui = useUiStore();
const query = ref('');
const inputRef = ref<HTMLInputElement | null>(null);

watch(query, (q) => {
  const trimmed = q.trim();
  // "@<query>" hands off to the command palette's server-backed search.
  if (trimmed.startsWith('@') && trimmed.length > 1) {
    ui.openPaletteWith(trimmed.slice(1));
    query.value = '';
    store.filterQuery = '';
    return;
  }
  store.filterQuery = trimmed.toLowerCase();
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

// Global "/" focuses the filter (unless already typing in a field).
function onGlobalKeydown(e: KeyboardEvent) {
  if (e.key !== '/' || e.metaKey || e.ctrlKey || e.altKey) return;
  const el = document.activeElement as HTMLElement | null;
  const tag = (el?.tagName || '').toUpperCase();
  if (tag === 'INPUT' || tag === 'TEXTAREA' || el?.isContentEditable) return;
  e.preventDefault();
  inputRef.value?.focus();
}

onMounted(() => document.addEventListener('keydown', onGlobalKeydown));
onUnmounted(() => document.removeEventListener('keydown', onGlobalKeydown));
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
      ref="inputRef"
      v-model="query"
      type="search"
      class="task-search-input"
      placeholder="Filter tasks… (/ to focus, @ to search server)"
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
