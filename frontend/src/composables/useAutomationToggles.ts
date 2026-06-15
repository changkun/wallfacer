import { ref } from 'vue';
import { api } from '../api/client';
import { useTaskStore } from '../stores/tasks';

// The five board automation switches, sourced from /api/config so the server
// is the single source of truth (not the shell env). Mirrors the legacy
// automation menu (ui/partials/automation-menu). Shared by the board
// AutomationMenu popover and the Execution settings tab so both read and
// write the same state.
export type AutomationKey =
  | 'autopilot'
  | 'autotest'
  | 'autosubmit'
  | 'autosync'
  | 'autopush';

export const AUTOMATION_KEYS: AutomationKey[] = [
  'autopilot',
  'autotest',
  'autosubmit',
  'autosync',
  'autopush',
];

export const automationLabels: Record<AutomationKey, string> = {
  autopilot: 'Implement',
  autotest: 'Test',
  autosubmit: 'Submit',
  autosync: 'Catch up',
  autopush: 'Push',
};

export const automationHints: Record<AutomationKey, string> = {
  autopilot: 'Auto-promote backlog tasks into In Progress',
  autotest: 'Run verification automatically',
  autosubmit: 'Mark waiting → done once verified',
  autosync: 'Rebase waiting tasks onto the default branch',
  autopush: 'Auto-push completed commits',
};

const busy = ref<Record<AutomationKey, boolean>>({
  autopilot: false,
  autotest: false,
  autosubmit: false,
  autosync: false,
  autopush: false,
});

export function useAutomationToggles() {
  const taskStore = useTaskStore();

  function isOn(k: AutomationKey): boolean {
    const cfg = taskStore.config;
    return !!(cfg && (cfg as unknown as Record<string, boolean>)[k]);
  }

  function isBusy(k: AutomationKey): boolean {
    return busy.value[k];
  }

  async function toggle(k: AutomationKey): Promise<void> {
    busy.value[k] = true;
    try {
      const next = !isOn(k);
      await api('PUT', '/api/config', { [k]: next });
      await taskStore.fetchConfig();
    } finally {
      busy.value[k] = false;
    }
  }

  // anyOn reports whether at least one switch is active, used to badge the
  // board automation button so the user can see automation is running.
  function anyOn(): boolean {
    return AUTOMATION_KEYS.some(isOn);
  }

  return { AUTOMATION_KEYS, automationLabels, automationHints, isOn, isBusy, toggle, anyOn };
}
