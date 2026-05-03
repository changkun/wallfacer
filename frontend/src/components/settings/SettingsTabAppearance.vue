<script setup lang="ts">
import { useTheme } from '../../composables/useTheme'

const { theme } = useTheme()
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
        role="group"
        aria-label="Theme mode"
      >
        <button
          type="button"
          data-mode="light"
          :class="{ active: theme === 'light' }"
          @click="theme = 'light'"
        >
          Light
        </button>
        <button
          type="button"
          data-mode="dark"
          :class="{ active: theme === 'dark' }"
          @click="theme = 'dark'"
        >
          Dark
        </button>
        <button
          type="button"
          data-mode="auto"
          :class="{ active: theme === 'auto' }"
          @click="theme = 'auto'"
        >
          Auto
        </button>
      </div>
    </div>
  </div>
</template>
