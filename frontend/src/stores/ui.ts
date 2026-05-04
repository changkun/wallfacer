import { defineStore } from 'pinia';
import { ref } from 'vue';

const SHOW_ARCHIVED_KEY = 'wallfacer-show-archived';

function readShowArchived(): boolean {
  try {
    if (typeof localStorage === 'undefined') return false;
    return localStorage.getItem(SHOW_ARCHIVED_KEY) === 'true';
  } catch { return false; }
}

export const useUiStore = defineStore('ui', () => {
  const showSettings = ref(false);
  const showWorkspaces = ref(false);
  const showPalette = ref(false);
  const showContainers = ref(false);
  const showInstructions = ref(false);
  const showSystemPrompts = ref(false);
  const showTemplates = ref(false);
  const showTerminal = ref(false);
  const showShortcuts = ref(false);
  const showArchived = ref(readShowArchived());

  function setShowArchived(v: boolean) {
    showArchived.value = v;
    try { if (typeof localStorage !== 'undefined') localStorage.setItem(SHOW_ARCHIVED_KEY, String(v)); }
    catch { /* ignore */ }
  }

  function openSettings() { showSettings.value = true; }
  function closeSettings() { showSettings.value = false; }
  function openWorkspaces() { showWorkspaces.value = true; }
  function closeWorkspaces() { showWorkspaces.value = false; }
  function openPalette() { showPalette.value = true; }
  function closePalette() { showPalette.value = false; }
  function openContainers() { showContainers.value = true; }
  function closeContainers() { showContainers.value = false; }
  function openInstructions() { showSettings.value = false; showInstructions.value = true; }
  function closeInstructions() { showInstructions.value = false; }
  function openSystemPrompts() { showSettings.value = false; showSystemPrompts.value = true; }
  function closeSystemPrompts() { showSystemPrompts.value = false; }
  function openTemplates() { showSettings.value = false; showTemplates.value = true; }
  function closeTemplates() { showTemplates.value = false; }
  function openTerminal() { showTerminal.value = true; }
  function closeTerminal() { showTerminal.value = false; }
  function toggleTerminal() { showTerminal.value = !showTerminal.value; }
  function openShortcuts() { showShortcuts.value = true; }
  function closeShortcuts() { showShortcuts.value = false; }

  return {
    showSettings, showWorkspaces, showPalette, showContainers,
    showInstructions, showSystemPrompts, showTemplates, showTerminal,
    showShortcuts, showArchived, setShowArchived,
    openSettings, closeSettings,
    openWorkspaces, closeWorkspaces,
    openPalette, closePalette,
    openContainers, closeContainers,
    openInstructions, closeInstructions,
    openSystemPrompts, closeSystemPrompts,
    openTemplates, closeTemplates,
    openTerminal, closeTerminal, toggleTerminal,
    openShortcuts, closeShortcuts,
  };
});
