import { defineStore } from 'pinia';
import { ref } from 'vue';

export const useUiStore = defineStore('ui', () => {
  const showSettings = ref(false);
  const showWorkspaces = ref(false);
  const showPalette = ref(false);
  const showContainers = ref(false);
  const showInstructions = ref(false);
  const showSystemPrompts = ref(false);
  const showTemplates = ref(false);

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

  return {
    showSettings, showWorkspaces, showPalette, showContainers,
    showInstructions, showSystemPrompts, showTemplates,
    openSettings, closeSettings,
    openWorkspaces, closeWorkspaces,
    openPalette, closePalette,
    openContainers, closeContainers,
    openInstructions, closeInstructions,
    openSystemPrompts, closeSystemPrompts,
    openTemplates, closeTemplates,
  };
});
