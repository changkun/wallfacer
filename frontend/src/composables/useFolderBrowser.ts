// Shared directory-browser logic for the workspace surfaces. Both the
// WorkspacePicker wizard and the WorkspaceEditModal need to walk the server's
// filesystem (GET /api/workspaces/browse) to pick project folders; this
// composable owns the pure navigation state and helpers so the two components
// share one implementation. Selection (which folders are chosen) stays in each
// component because it differs per surface — the wizard collects a new set, the
// edit modal mutates an existing workspace.
import { ref } from 'vue';

import { api } from '../api/client';

export interface BrowseEntry {
  name: string;
  path: string;
  is_git_repo: boolean;
}

interface BrowseResponse {
  path: string;
  entries: BrowseEntry[];
}

export function useFolderBrowser() {
  const browsePath = ref('/');
  const pathInput = ref('/');
  const browseEntries = ref<BrowseEntry[]>([]);
  const browseLoading = ref(false);
  const browseError = ref('');
  const filter = ref('');
  const showHidden = ref(false);

  // browse loads the directory listing for `path`. An empty path makes the
  // backend resolve to the user's home directory (the first-run landing spot).
  async function browse(path: string) {
    browseLoading.value = true;
    browseError.value = '';
    try {
      const res = await api<BrowseResponse>(
        'GET',
        `/api/workspaces/browse?path=${encodeURIComponent(path)}`,
      );
      browsePath.value = res.path;
      pathInput.value = res.path;
      browseEntries.value = res.entries;
    } catch (e: unknown) {
      browseError.value = e instanceof Error ? e.message : 'Failed to browse directory';
      browseEntries.value = [];
    } finally {
      browseLoading.value = false;
    }
  }

  function navigateUp() {
    const parent = browsePath.value.split('/').slice(0, -1).join('/') || '/';
    browse(parent);
  }

  function navigateInto(entry: BrowseEntry) {
    browse(entry.path);
  }

  function goToPath() {
    if (pathInput.value.trim()) browse(pathInput.value.trim());
  }

  function onPathKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault();
      goToPath();
    }
  }

  // Collapse a home directory prefix to '~' for display (full path stays in title).
  function shortenPath(path: string) {
    const m = path.match(/^(\/(?:Users|home)\/[^/]+|[A-Z]:\\Users\\[^\\]+)/);
    if (m) return '~' + path.substring(m[1].length);
    return path;
  }

  function breadcrumbSegments() {
    const parts = browsePath.value.split('/').filter(Boolean);
    const segs: { label: string; path: string }[] = [{ label: '/', path: '/' }];
    let acc = '';
    for (const p of parts) {
      acc += `/${p}`;
      segs.push({ label: p, path: acc });
    }
    return segs;
  }

  return {
    browsePath, pathInput, browseEntries, browseLoading, browseError, filter, showHidden,
    browse, navigateUp, navigateInto, goToPath, onPathKeydown, shortenPath, breadcrumbSegments,
  };
}
