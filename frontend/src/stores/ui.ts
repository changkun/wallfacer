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
  const showWorkspaces = ref(false);
  // Id of the workspace whose settings popup (WorkspaceEditModal) is open, or
  // null when closed. Opened from the sidebar switcher and the picker's per-row
  // Edit. A single app-level modal reads this so any surface can raise it.
  const editWorkspaceId = ref<string | null>(null);
  const showPalette = ref(false);
  const showSystemPrompts = ref(false);
  // Terminal visibility now lives in the dock store (the terminal is a dockable
  // panel). These delegate so existing callers keep working unchanged.
  const showTerminal = computed(() => useDockStore().terminalOpen);
  const showExplorer = ref(false);
  const showTrash = ref(false);
  const showShortcuts = ref(false);
  const showArchived = ref(readShowArchived());

  // True while a workspace switch is in flight. AppLayout renders a full-UI
  // blocking overlay (above every modal) so the user never sees the new active
  // state painted over stale old content mid-switch.
  const switchingWorkspace = ref(false);

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

  function openWorkspaces() { showWorkspaces.value = true; }
  function openWorkspaceEdit(id: string) { editWorkspaceId.value = id; }
  function closeWorkspaceEdit() { editWorkspaceId.value = null; }
  const paletteSeed = ref('');
  function openPaletteWith(seed: string) { paletteSeed.value = seed; showPalette.value = true; }
  function openTerminal() { useDockStore().openTerminal(); }
  function closeTerminal() { useDockStore().closeTerminal(); }
  function toggleTerminal() { useDockStore().toggleTerminal(); }
  function openExplorer() { showExplorer.value = true; }
  function closeExplorer() { showExplorer.value = false; }
  function toggleExplorer() { showExplorer.value = !showExplorer.value; }
  function openTrash() { showTrash.value = true; }
  function openShortcuts() { showShortcuts.value = true; }

  return {
    showWorkspaces, editWorkspaceId, showPalette,
    showSystemPrompts, showTerminal,
    showExplorer, showTrash, showShortcuts, showArchived, setShowArchived,
    switchingWorkspace,
    dispatchedIds, markDispatched, consumeDispatched,
    paletteSeed,
    openWorkspaces,
    openWorkspaceEdit, closeWorkspaceEdit,
    openPaletteWith,
    openTerminal, closeTerminal, toggleTerminal,
    openExplorer, closeExplorer, toggleExplorer,
    openTrash,
    openShortcuts,
  };
});
