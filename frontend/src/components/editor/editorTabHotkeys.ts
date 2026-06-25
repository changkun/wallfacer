// Window-level hotkeys for the board's editor tab strip. These run on the
// window (not the CodeMirror view) so they fire even when focus is on the
// Explorer tree after a single-click — the case where CodeMirror's own Mod-s
// keymap never sees the key and Cmd/Ctrl+S would otherwise fall through to the
// browser's "save page" dialog.
import { BOARD_TAB_ID } from '../../stores/editorTabs';

interface TabHotkeyTarget {
  activeId: string;
  save(path: string): Promise<void> | void;
  close(path: string): Promise<void> | void;
}

// Returns true when the key was handled. Cmd/Ctrl+S pins (and, if dirty, saves)
// the active file tab; the key is always swallowed so the browser dialog never
// appears, even on the pinned board where it no-ops. Cmd/Ctrl+W closes the
// active file tab (the board is pinned and ignored).
export function handleTabHotkey(e: KeyboardEvent, tabs: TabHotkeyTarget): boolean {
  if (!(e.metaKey || e.ctrlKey)) return false;
  const k = e.key.toLowerCase();
  if (k === 's') {
    e.preventDefault();
    if (tabs.activeId !== BOARD_TAB_ID) void tabs.save(tabs.activeId);
    return true;
  }
  if (k === 'w' && tabs.activeId !== BOARD_TAB_ID) {
    e.preventDefault();
    void tabs.close(tabs.activeId);
    return true;
  }
  return false;
}
