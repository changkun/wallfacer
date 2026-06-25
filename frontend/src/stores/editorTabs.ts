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
  // Preview (VS Code "temporary") tab: a single-click opens here in italics and
  // the next single-click reuses this slot. Saving, double-click, or editing-
  // then-navigating-away promotes it to a permanent (kept) tab.
  preview: boolean;
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

  // Open a file tab, or focus it if already open (no duplicate). Single-click
  // (preview, the default) opens a temporary tab that the next preview reuses;
  // pass { preview: false } (double-click) to open a kept tab. Content is
  // fetched lazily; the tab appears immediately in a loading state.
  async function openFile(
    workspace: string,
    path: string,
    opts: { preview?: boolean } = {},
  ): Promise<void> {
    const asPreview = opts.preview ?? true;
    const existing = find(path);
    if (existing) {
      activeId.value = path;
      if (!asPreview) existing.preview = false; // an explicit open pins it
      return;
    }
    // Reuse the existing preview slot: a clean preview is replaced in place; a
    // dirty preview is kept (promoted) so its edits survive.
    let insertAt = tabs.value.length;
    if (asPreview) {
      const pvIdx = tabs.value.findIndex((t) => t.preview);
      if (pvIdx >= 0) {
        const pv = tabs.value[pvIdx];
        if (pv.content !== pv.baseline) {
          pv.preview = false;
        } else {
          tabs.value.splice(pvIdx, 1);
          insertAt = pvIdx;
        }
      }
    }
    tabs.value.splice(insertAt, 0, {
      path,
      workspace,
      name: basename(path),
      content: '',
      baseline: '',
      preview: asPreview,
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

  // Promote a preview tab to a permanent (kept) one. Triggered by saving and by
  // double-click.
  function promote(path: string): void {
    const t = find(path);
    if (t) t.preview = false;
  }

  function setContent(path: string, text: string): void {
    const t = find(path);
    if (t) t.content = text;
  }

  async function save(path: string): Promise<void> {
    const t = find(path);
    if (!t || t.saving || t.loading) return;
    // A clean tab has nothing to write: just pin it (Cmd/Ctrl+S on a freshly
    // opened preview). Skipping the PUT avoids touching the file's mtime and
    // tripping the file-watch stream into a spurious reload.
    if (t.content === t.baseline) {
      t.preview = false;
      return;
    }
    t.saving = true;
    t.saveError = null;
    try {
      await api('PUT', '/api/explorer/file', { workspace: t.workspace, path: t.path, content: t.content });
      t.baseline = t.content;
      t.preview = false; // saving keeps the tab (VS Code preview → permanent)
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
    promote,
    setContent,
    save,
    close,
  };
});
