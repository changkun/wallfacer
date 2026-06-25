// Source of truth for the board's VS Code-style editor tabs. Lives in a store
// (not BoardPage) so open files, their buffers, and dirty state survive
// BoardPage unmounting (navigating to /chat and back) and editor DOM teardown:
// the editor component rehydrates its text from `tab.content` on (re)mount.
//
// The board is a synthetic, pinned, non-closeable tab (id `board`) that is
// always the implicit first tab; file tabs key off their workspace-relative
// path. See specs/local/inline-file-panel.md.
import { defineStore } from 'pinia';
import { ref, computed } from 'vue';
import { api } from '../api/client';
import { useDialogStore } from './dialog';

export const BOARD_TAB_ID = 'board';

export interface FileTab {
  path: string;        // workspace-relative path; tab identity
  workspace: string;
  name: string;        // basename; disambiguated by parent dir on collision
  content: string;     // live buffer; the editor reads/writes this
  baseline: string;    // last-saved content; dirty = content !== baseline
  loading: boolean;
  loadError: string | null;
  saving: boolean;
  saveError: string | null;
}

function basename(path: string): string {
  const parts = path.split('/');
  return parts[parts.length - 1] || path;
}

export const useEditorTabsStore = defineStore('editorTabs', () => {
  const tabs = ref<FileTab[]>([]);
  const activeId = ref<string>(BOARD_TAB_ID);

  function find(path: string): FileTab | undefined {
    return tabs.value.find((t) => t.path === path);
  }

  // Tab label, disambiguated by immediate parent directory only when two open
  // tabs share a basename (e.g. two `index.ts`).
  function labelFor(tab: FileTab): string {
    const collides = tabs.value.some((t) => t !== tab && t.name === tab.name);
    if (!collides) return tab.name;
    const parent = tab.path.split('/').slice(-2, -1)[0];
    return parent ? `${parent}/${tab.name}` : tab.name;
  }

  function isDirty(path: string): boolean {
    const t = find(path);
    return !!t && t.content !== t.baseline;
  }

  const anyDirty = computed(() => tabs.value.some((t) => t.content !== t.baseline));

  // Open a file tab, or focus it if already open (no duplicate). The content is
  // fetched lazily; the tab appears immediately in a loading state.
  async function openFile(workspace: string, path: string): Promise<void> {
    const existing = find(path);
    if (existing) {
      activeId.value = path;
      return;
    }
    tabs.value.push({
      path,
      workspace,
      name: basename(path),
      content: '',
      baseline: '',
      loading: true,
      loadError: null,
      saving: false,
      saveError: null,
    });
    activeId.value = path;
    // Mutate through the reactive proxy (find), not the pushed raw object — Vue
    // wraps array elements in a proxy, so writing the raw reference changes the
    // value without firing reactivity, leaving the editor stuck on "Loading…".
    const live = find(path);
    if (!live) return;
    try {
      const url = `/api/explorer/file?workspace=${encodeURIComponent(workspace)}&path=${encodeURIComponent(path)}`;
      const res = await api<{ content: string }>('GET', url);
      const text = typeof res === 'string' ? res : (res.content ?? JSON.stringify(res, null, 2));
      live.content = text;
      live.baseline = text;
    } catch (e: unknown) {
      live.loadError = e instanceof Error ? e.message : 'Failed to load file.';
    } finally {
      live.loading = false;
    }
  }

  function focus(id: string): void {
    if (id === BOARD_TAB_ID || find(id)) activeId.value = id;
  }

  function setContent(path: string, text: string): void {
    const t = find(path);
    if (t) t.content = text;
  }

  async function save(path: string): Promise<void> {
    const t = find(path);
    if (!t || t.saving || t.loading) return;
    t.saving = true;
    t.saveError = null;
    try {
      await api('PUT', '/api/explorer/file', { workspace: t.workspace, path: t.path, content: t.content });
      t.baseline = t.content;
    } catch (e: unknown) {
      t.saveError = e instanceof Error ? e.message : 'Failed to save file.';
    } finally {
      t.saving = false;
    }
  }

  // Close a file tab. The board tab is pinned and never closes. A dirty tab runs
  // the shared discard guard first; declining keeps the tab open. When the
  // active tab closes, focus falls to its right neighbour, else left, else the
  // board.
  async function close(id: string): Promise<void> {
    if (id === BOARD_TAB_ID) return;
    const idx = tabs.value.findIndex((t) => t.path === id);
    if (idx < 0) return;
    const t = tabs.value[idx];
    if (t.content !== t.baseline) {
      const dialog = useDialogStore();
      const ok = await dialog.confirm({
        title: 'Discard changes?',
        message: `You have unsaved edits to ${t.name}. Discard them?`,
        confirmLabel: 'Discard',
        cancelLabel: 'Keep editing',
        danger: true,
      });
      if (!ok) return;
    }
    tabs.value.splice(idx, 1);
    if (activeId.value === id) {
      const next = tabs.value[idx] ?? tabs.value[idx - 1];
      activeId.value = next ? next.path : BOARD_TAB_ID;
    }
  }

  return {
    tabs,
    activeId,
    labelFor,
    find,
    isDirty,
    anyDirty,
    openFile,
    focus,
    setContent,
    save,
    close,
  };
});
