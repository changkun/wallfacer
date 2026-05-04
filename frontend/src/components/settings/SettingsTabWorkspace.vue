<script setup lang="ts">
import { ref, computed } from 'vue';
import { api } from '../../api/client';
import { useTaskStore } from '../../stores/tasks';
import type { WorkspaceGroup } from '../../api/types';

const emit = defineEmits<{ workspaces: [] }>();
const store = useTaskStore();
const status = ref('');
const switchingKey = ref<string | null>(null);

const activeWorkspaces = computed(() => store.config?.workspaces ?? []);
const workspaceGroups = computed<WorkspaceGroup[]>(() => store.config?.workspace_groups ?? []);

function workspaceGroupLabel(group: WorkspaceGroup): string {
  if (!group || !Array.isArray(group.workspaces) || !group.workspaces.length) return 'Empty group';
  if (group.name) return group.name;
  const names = group.workspaces.map(p => {
    const clean = String(p || '').replace(/[\\/]+$/, '');
    const parts = clean.split(/[\\/]/);
    return parts[parts.length - 1] || clean;
  });
  return names.join(' + ');
}

function isActiveGroup(group: WorkspaceGroup): boolean {
  const a = group.workspaces;
  const b = activeWorkspaces.value;
  if (!Array.isArray(a) || a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
  return true;
}

async function useGroup(group: WorkspaceGroup) {
  if (switchingKey.value) return;
  switchingKey.value = group.key;
  status.value = '';
  try {
    await api('PUT', '/api/workspaces', { workspaces: group.workspaces });
    await store.fetchConfig();
    await store.fetchTasks();
  } catch (e) {
    status.value = 'Error: ' + (e instanceof Error ? e.message : String(e));
  } finally {
    switchingKey.value = null;
  }
}
</script>

<template>
  <div class="settings-tab-content active" data-settings-tab="workspace">
    <div class="settings-section">
      <div style="margin-bottom: 8px; font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px;">
        Active Workspaces
      </div>
      <div
        id="settings-workspace-list"
        style="display: flex; flex-direction: column; gap: 6px; font-size: 12px; color: var(--text-secondary); margin-bottom: 10px;"
      >
        <div
          v-if="activeWorkspaces.length === 0"
          style="color: var(--text-muted)"
        >No workspaces configured.</div>
        <div
          v-for="path in activeWorkspaces"
          :key="path"
          style="font-family: monospace; font-size: 11px; padding: 6px 8px; border: 1px solid var(--border); border-radius: 6px; background: var(--bg-elevated);"
        >{{ path }}</div>
      </div>
      <div style="display: flex; gap: 8px; align-items: center;">
        <button
          type="button"
          class="btn-icon"
          style="font-size: 12px; padding: 4px 10px;"
          @click="emit('workspaces')"
        >Change</button>
        <span
          id="settings-workspace-status"
          style="font-size: 11px; color: var(--text-muted)"
        >{{ status }}</span>
      </div>
    </div>

    <div class="settings-section">
      <div style="margin-bottom: 8px; font-size: 11px; font-weight: 600; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.5px;">
        Saved Workspace Groups
      </div>
      <div
        id="settings-workspace-groups"
        style="display: flex; flex-direction: column; gap: 8px; font-size: 12px; color: var(--text-secondary);"
      >
        <div
          v-if="workspaceGroups.length === 0"
          style="color: var(--text-muted); font-size: 11px"
        >Saved workspace groups will appear here after you switch boards.</div>
        <div
          v-for="group in workspaceGroups"
          :key="group.key"
          style="border: 1px solid var(--border); border-radius: 8px; padding: 8px; background: var(--bg-elevated); display: flex; flex-direction: column; gap: 8px;"
        >
          <div style="display: flex; align-items: center; justify-content: space-between; gap: 8px;">
            <div style="font-size: 12px; font-weight: 600;">
              {{ workspaceGroupLabel(group) }}
              <span
                v-if="isActiveGroup(group)"
                style="font-size: 10px; color: var(--text-muted); font-weight: 500; margin-left: 4px;"
              >Current</span>
            </div>
            <div style="display: flex; gap: 6px; align-items: center;">
              <button
                type="button"
                class="btn-icon"
                style="font-size: 11px; padding: 3px 8px;"
                :disabled="switchingKey !== null"
                @click="useGroup(group)"
              >{{ switchingKey === group.key ? 'Switching…' : 'Use' }}</button>
            </div>
          </div>
          <div style="display: flex; flex-direction: column; gap: 4px;">
            <div
              v-for="path in group.workspaces"
              :key="path"
              style="font-family: monospace; font-size: 11px; color: var(--text-muted); word-break: break-all;"
            >{{ path }}</div>
          </div>
        </div>
      </div>
      <div style="margin-top: 6px; font-size: 11px; color: var(--text-muted); line-height: 1.4;">
        Workspace groups are saved automatically when they become active, so you can switch back without rebuilding the same folder set.
      </div>
    </div>
  </div>
</template>
