// Board "unread" detection (ports ui/js/sidebar-badge.js): a dot appears on the
// Board nav when new task ids arrive while the user is looking at another view.

export function hasUnseen(ids: string[], seen: Set<string>): boolean {
  for (const id of ids) {
    if (!seen.has(id)) return true;
  }
  return false;
}
