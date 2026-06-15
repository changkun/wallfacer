import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import { useDockStore } from './dock';

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
  const showSystemPrompts = ref(false);
  const showTemplates = ref(false);
  // Terminal visibility now lives in the dock store (the terminal is a dockable
  // panel). These delegate so existing callers keep working unchanged.
  const showTerminal = computed(() => useDockStore().terminalOpen);
  const showExplorer = ref(false);
  const showTrash = ref(false);
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
  function openSystemPrompts() { showSettings.value = false; showSystemPrompts.value = true; }
  function closeSystemPrompts() { showSystemPrompts.value = false; }
  function openTemplates() { showSettings.value = false; showTemplates.value = true; }
  function closeTemplates() { showTemplates.value = false; }
  function openTerminal() { useDockStore().openTerminal(); }
  function closeTerminal() { useDockStore().closeTerminal(); }
  function toggleTerminal() { useDockStore().toggleTerminal(); }
  function openExplorer() { showExplorer.value = true; }
  function closeExplorer() { showExplorer.value = false; }
  function toggleExplorer() { showExplorer.value = !showExplorer.value; }
  function openTrash() { showTrash.value = true; }
  function closeTrash() { showTrash.value = false; }
  function openShortcuts() { showShortcuts.value = true; }
  function closeShortcuts() { showShortcuts.value = false; }

  return {
    showSettings, showWorkspaces, showPalette,
    showSystemPrompts, showTemplates, showTerminal,
    showExplorer, showTrash, showShortcuts, showArchived, setShowArchived,
    dispatchedIds, markDispatched, consumeDispatched,
    paletteSeed,
    openSettings, closeSettings,
    openWorkspaces, closeWorkspaces,
    openPalette, openPaletteWith, closePalette,

    openSystemPrompts, closeSystemPrompts,
    openTemplates, closeTemplates,
    openTerminal, closeTerminal, toggleTerminal,
    openExplorer, closeExplorer, toggleExplorer,
    openTrash, closeTrash,
    openShortcuts, closeShortcuts,
  };
});
