// Maps the raw /api/explorer/tree response into the tree-node shape the
// ExplorerPage renders. The backend returns one level of a directory as
// `{ name, type, size, modified }` entries — it does NOT send an absolute
// `path` or an `is_dir` flag. The client reconstructs both: `path` from the
// parent request path + entry name (matching the legacy ui/js/explorer.js
// behaviour), and `is_dir` from `type === 'dir'`. Directories sort before
// files, then case-insensitive alphabetical.

export interface RawExplorerEntry {
  name: string;
  type: string; // "dir" | "file"
  size?: number;
  modified?: string;
}

export interface TreeEntry {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
}

/** Join a parent directory path with a child name, collapsing any trailing
 *  slashes on the parent so the result has exactly one separator. */
export function joinPath(parent: string, name: string): string {
  return parent.replace(/\/+$/, '') + '/' + name;
}

export function mapEntries(reqPath: string, raw: RawExplorerEntry[] | undefined | null): TreeEntry[] {
  const mapped: TreeEntry[] = (raw ?? []).map((e) => ({
    name: e.name,
    path: joinPath(reqPath, e.name),
    is_dir: e.type === 'dir',
    size: e.size ?? 0,
  }));
  mapped.sort((a, b) => {
    if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1;
    return a.name.localeCompare(b.name);
  });
  return mapped;
}
