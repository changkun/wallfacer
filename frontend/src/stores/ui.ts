import { defineStore } from 'pinia';
import { ref } from 'vue';

export const useUiStore = defineStore('ui', () => {
  const showSettings = ref(false);
  const showWorkspaces = ref(false);
  const showPalette = ref(false);
  const showContainers = ref(false);

  function openSettings() { showSettings.value = true; }
  function closeSettings() { showSettings.value = false; }
  function openWorkspaces() { showWorkspaces.value = true; }
  function closeWorkspaces() { showWorkspaces.value = false; }
  function openPalette() { showPalette.value = true; }
  function closePalette() { showPalette.value = false; }
  function openContainers() { showContainers.value = true; }
  function closeContainers() { showContainers.value = false; }

  return {
    showSettings,
    showWorkspaces,
    showPalette,
    showContainers,
    openSettings,
    closeSettings,
    openWorkspaces,
    closeWorkspaces,
    openPalette,
    closePalette,
    openContainers,
    closeContainers,
  };
});
