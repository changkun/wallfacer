<script setup lang="ts">
import { useTheme } from '../../composables/useTheme';
import { useUiStore } from '../../stores/ui';

const { theme } = useTheme();
const ui = useUiStore();
</script>

<template>
  <div class="settings-tab-content active" data-settings-tab="appearance">
    <div class="settings-card">
      <div class="settings-card-head">
        <h4>Theme</h4>
        <p>Choose the interface color mode for the current session.</p>
      </div>
      <div
        class="theme-switch settings-theme-switch"
        id="theme-switch"
        role="group"
        aria-label="Theme mode"
      >
        <button
          type="button"
          data-mode="light"
          :class="{ active: theme === 'light' }"
          @click="theme = 'light'"
        >Light</button>
        <button
          type="button"
          data-mode="dark"
          :class="{ active: theme === 'dark' }"
          @click="theme = 'dark'"
        >Dark</button>
        <button
          type="button"
          data-mode="auto"
          :class="{ active: theme === 'auto' }"
          @click="theme = 'auto'"
        >Auto</button>
      </div>
    </div>

    <div class="settings-card">
      <div class="settings-card-head">
        <h4>Done Column</h4>
        <p>Toggle visibility of archived completed tasks on the board.</p>
      </div>
      <label class="settings-toggle">
        <input
          type="checkbox"
          id="show-archived-toggle"
          :checked="ui.showArchived"
          @change="ui.setShowArchived(($event.target as HTMLInputElement).checked)"
        />
        Show archived tasks
      </label>
    </div>
  </div>
</template>
