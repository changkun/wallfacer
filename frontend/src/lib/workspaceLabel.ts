// Workspace label helpers shared by the sidebar switcher button and the
// group popover rows. A group without an explicit name falls back to the
// basename(s) of its paths; using the raw path truncates uselessly to
// "/Users/ch..." in the narrow popover.

/** Last path segment, trailing slashes ignored. Returns the input if empty. */
export function basename(p: string): string {
  const parts = p.replace(/\/+$/, '').split('/');
  return parts[parts.length - 1] || p;
}

/** Readable label for a workspace group: explicit name, else basename(s)
 * joined with " + ". Returns "Workspace" when there is nothing to show. */
export function groupLabel(g: { name?: string; workspaces: string[] }): string {
  if (g.name) return g.name;
  if (!g.workspaces || g.workspaces.length === 0) return 'Workspace';
  return g.workspaces.map(basename).join(' + ');
}

/** Display label for a first-class workspace: the trimmed name, else the folder
 * basenames joined with " + " (matching the sidebar/group convention), else a
 * never-blank placeholder. Used everywhere a workspace is shown so an unnamed
 * workspace never renders empty. */
export function workspaceLabel(name: string | undefined, folders: string[]): string {
  const trimmed = name?.trim();
  if (trimmed) return trimmed;
  if (folders && folders.length) return folders.map(basename).join(' + ');
  return 'Untitled workspace';
}
