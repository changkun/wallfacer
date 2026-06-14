<script setup lang="ts">
import { useUiStore } from '../../stores/ui';

const ui = useUiStore();
</script>

<template>
  <div class="settings-tab-content active" data-settings-tab="appearance">
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
