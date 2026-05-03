<script setup lang="ts">
import { useUiStore } from '../../stores/ui';

const ui = useUiStore();
</script>

<template>
  <div class="settings-tab-content active" data-settings-tab="prompts">
    <div class="settings-section">
      <div
        style="
          margin-bottom: 8px;
          font-size: 11px;
          font-weight: 600;
          color: var(--text-muted);
          text-transform: uppercase;
          letter-spacing: 0.5px;
        "
      >
        AGENTS.md
      </div>
      <button
        type="button"
        class="btn-icon"
        style="font-size: 12px; padding: 4px 10px"
        @click="ui.openInstructions()"
      >
        Edit
      </button>
      <div
        style="
          margin-top: 6px;
          font-size: 11px;
          color: var(--text-muted);
          line-height: 1.4;
        "
      >
        Workspace instructions for the agent.
      </div>
    </div>

    <div class="settings-section">
      <div
        style="
          margin-bottom: 8px;
          font-size: 11px;
          font-weight: 600;
          color: var(--text-muted);
          text-transform: uppercase;
          letter-spacing: 0.5px;
        "
      >
        System Prompt Templates
      </div>
      <button
        type="button"
        class="btn-icon"
        style="font-size: 12px; padding: 4px 10px"
        @click="ui.openSystemPrompts()"
      >
        Manage
      </button>
      <div
        style="
          margin-top: 6px;
          font-size: 11px;
          color: var(--text-muted);
          line-height: 1.4;
        "
      >
        Override the 7 built-in agent prompt templates. Drop
        <code style="font-family: monospace">.tmpl</code> files in the prompts
        directory to customize how tasks are instructed.
      </div>
    </div>

    <div class="settings-section">
      <div
        style="
          margin-bottom: 8px;
          font-size: 11px;
          font-weight: 600;
          color: var(--text-muted);
          text-transform: uppercase;
          letter-spacing: 0.5px;
        "
      >
        Prompt Templates
      </div>
      <button
        type="button"
        class="btn-icon"
        style="font-size: 12px; padding: 4px 10px"
        @click="ui.openTemplates()"
      >
        Manage
      </button>
      <div
        style="
          margin-top: 6px;
          font-size: 11px;
          color: var(--text-muted);
          line-height: 1.4;
        "
      >
        Save and reuse named prompt fragments when creating tasks.
      </div>
    </div>
  </div>
</template>
