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
  const showInstructions = ref(false);
  const showSystemPrompts = ref(false);
  const showTemplates = ref(false);
  const showTerminal = ref(false);
  const showShortcuts = ref(false);
  const showArchived = ref(readShowArchived());

  // Task ids freshly dispatched from Plan mode; a TaskCard consumes its own id
  // on mount to play a one-shot "just created" pulse, even after navigating to
  // the board (mirrors ui/js/dispatch-toast.js highlight).
  const dispatchedIds = ref<Set<string>>(new Set());
  function markDispatched(ids: string[]) {
    if (!ids.length) return;
    const next = new Set(dispatchedIds.value);
    for (const id of ids) next.add(id);
    dispatchedIds.value = next;
  }
  function consumeDispatched(id: string) {
    if (!dispatchedIds.value.has(id)) return;
    const next = new Set(dispatchedIds.value);
    next.delete(id);
    dispatchedIds.value = next;
  }

  function setShowArchived(v: boolean) {
    showArchived.value = v;
    try { if (typeof localStorage !== 'undefined') localStorage.setItem(SHOW_ARCHIVED_KEY, String(v)); }
    catch { /* ignore */ }
  }

  function openSettings() { showSettings.value = true; }
  function closeSettings() { showSettings.value = false; }
  function openWorkspaces() { showWorkspaces.value = true; }
  function closeWorkspaces() { showWorkspaces.value = false; }
  const paletteSeed = ref('');
  function openPalette() { showPalette.value = true; }
  function openPaletteWith(seed: string) { paletteSeed.value = seed; showPalette.value = true; }
  function closePalette() { showPalette.value = false; }
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
    showSettings, showWorkspaces, showPalette,
    showInstructions, showSystemPrompts, showTemplates, showTerminal,
    showShortcuts, showArchived, setShowArchived,
    dispatchedIds, markDispatched, consumeDispatched,
    paletteSeed,
    openSettings, closeSettings,
    openWorkspaces, closeWorkspaces,
    openPalette, openPaletteWith, closePalette,

    openInstructions, closeInstructions,
    openSystemPrompts, closeSystemPrompts,
    openTemplates, closeTemplates,
    openTerminal, closeTerminal, toggleTerminal,
    openShortcuts, closeShortcuts,
  };
});
