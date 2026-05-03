<script setup lang="ts">
import { ref } from 'vue';
import { api } from '../../api/client';
import { useTaskStore } from '../../stores/tasks';

defineEmits<{ workspaces: [] }>();
const store = useTaskStore();
const status = ref('');

async function switchToGroup(workspaces: string[]) {
  await api('PUT', '/api/workspaces', { workspaces });
  await store.fetchConfig();
  await store.fetchTasks();
}
</script>

<template>
  <div class="settings-tab-content active" data-settings-tab="workspace">
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
        Active Workspaces
      </div>
      <div
        style="
          display: flex;
          flex-direction: column;
          gap: 6px;
          margin-bottom: 10px;
        "
      >
        <div
          v-for="path in store.config?.workspaces ?? []"
          :key="path"
          style="
            font-family: monospace;
            font-size: 12px;
            color: var(--text-secondary);
            word-break: break-all;
          "
        >
          {{ path }}
        </div>
      </div>
      <div style="display: flex; gap: 8px; align-items: center">
        <button
          type="button"
          class="btn-icon"
          style="font-size: 12px; padding: 4px 10px"
          @click="$emit('workspaces')"
        >
          Change
        </button>
        <span style="font-size: 11px; color: var(--text-muted)">{{ status }}</span>
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
        Saved Workspace Groups
      </div>
      <div
        style="
          display: flex;
          flex-direction: column;
          gap: 8px;
          font-size: 12px;
          color: var(--text-secondary);
        "
      >
        <div
          v-for="group in store.config?.workspace_groups ?? []"
          :key="group.key"
          style="
            display: flex;
            align-items: center;
            gap: 8px;
            flex-wrap: wrap;
          "
        >
          <span v-if="group.name" style="font-weight: 600; color: var(--text-primary)">
            {{ group.name }}
          </span>
          <span style="font-family: monospace; font-size: 12px; word-break: break-all">
            {{ group.workspaces.join(' · ') }}
          </span>
          <span
            style="
              font-family: monospace;
              font-size: 10px;
              color: var(--text-muted);
              padding: 1px 6px;
              border: 1px solid var(--border-color);
              border-radius: 3px;
            "
          >
            {{ group.key }}
          </span>
          <span
            v-if="group.max_parallel"
            style="font-size: 11px; color: var(--text-muted)"
          >
            max_parallel: {{ group.max_parallel }}
          </span>
          <button
            type="button"
            class="btn-icon"
            style="font-size: 12px; padding: 4px 10px"
            @click="switchToGroup(group.workspaces)"
          >
            Switch
          </button>
        </div>
      </div>
      <div
        style="
          margin-top: 6px;
          font-size: 11px;
          color: var(--text-muted);
          line-height: 1.4;
        "
      >
        Workspace groups are saved automatically when they become active, so you
        can switch back without rebuilding the same folder set.
      </div>
    </div>
  </div>
</template>
