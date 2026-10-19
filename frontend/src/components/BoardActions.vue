<script setup lang="ts">
// The board's topbar actions: task search, explorer toggle, trash. Registered
// by BoardPage on the ui store and rendered by Topbar, so the page owns the
// controls while the bar owns their place.
import SearchBar from './SearchBar.vue';
import { useUiStore } from '../stores/ui';

const ui = useUiStore();
</script>

<template>
  <SearchBar />
  <button
    type="button"
    class="icon-btn"
    :class="{ on: ui.showExplorer }"
    :title="ui.showExplorer ? 'Close Explorer' : 'Open Explorer'"
    :aria-pressed="ui.showExplorer"
    data-action="explorer"
    @click="ui.toggleExplorer()"
  >
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path>
      <line x1="9" y1="9" x2="9" y2="21"></line>
    </svg>
  </button>
  <button
    type="button"
    class="icon-btn"
    title="Trash"
    aria-label="Trash"
    data-action="trash"
    @click="ui.openTrash()"
  >
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
      <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"></path>
    </svg>
  </button>
</template>
