// The window-level Cmd/Ctrl+S handler is the fix for: single-click a file
// (Explorer-focused, CodeMirror unfocused) → Ctrl+S pops the browser "save
// page" dialog instead of pinning the tab. These pin that the handler swallows
// the key (preventDefault) and saves the active file tab.
import { describe, it, expect, vi } from 'vitest';
import { handleTabHotkey } from './editorTabHotkeys';
import { BOARD_TAB_ID } from '../../stores/editorTabs';

function target(activeId: string) {
  return { activeId, save: vi.fn(), close: vi.fn() };
}

function key(k: string): KeyboardEvent {
  return new KeyboardEvent('keydown', { key: k, metaKey: true, cancelable: true });
}

describe('handleTabHotkey', () => {
  it('Cmd/Ctrl+S saves the active file tab and prevents the browser dialog', () => {
    const tabs = target('src/a.ts');
    const e = key('s');
    expect(handleTabHotkey(e, tabs)).toBe(true);
    expect(e.defaultPrevented).toBe(true); // without preventDefault the browser saves the page
    expect(tabs.save).toHaveBeenCalledWith('src/a.ts');
  });

  it('Cmd/Ctrl+S on the board swallows the key but saves nothing', () => {
    const tabs = target(BOARD_TAB_ID);
    const e = key('s');
    expect(handleTabHotkey(e, tabs)).toBe(true);
    expect(e.defaultPrevented).toBe(true);
    expect(tabs.save).not.toHaveBeenCalled();
  });

  it('Cmd/Ctrl+W closes the active file tab, ignores the board', () => {
    const tabs = target('src/a.ts');
    const e = key('w');
    expect(handleTabHotkey(e, tabs)).toBe(true);
    expect(e.defaultPrevented).toBe(true);
    expect(tabs.close).toHaveBeenCalledWith('src/a.ts');

    const boardTabs = target(BOARD_TAB_ID);
    const be = key('w');
    expect(handleTabHotkey(be, boardTabs)).toBe(false);
    expect(be.defaultPrevented).toBe(false);
    expect(boardTabs.close).not.toHaveBeenCalled();
  });

  it('ignores keys without a modifier and unrelated keys', () => {
    const tabs = target('src/a.ts');
    const plain = new KeyboardEvent('keydown', { key: 's', cancelable: true });
    expect(handleTabHotkey(plain, tabs)).toBe(false);
    expect(handleTabHotkey(key('x'), tabs)).toBe(false);
    expect(tabs.save).not.toHaveBeenCalled();
  });
});
