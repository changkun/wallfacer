// Remembers the route the user last visited so a cold app launch can return
// them there instead of always dropping onto the board. Only the local-mode
// app uses this (cloud `/` is the marketing page); see main.ts for the wiring.
//
// We store the full path (including query), so the focused spec (`?spec=`),
// open task (`?task=`), and editor tab (`?tab=`) come back for free. A stale
// path degrades gracefully: PlanPage ignores a `?spec=` it can't resolve in
// the current spec tree, and the needsWorkspace gate covers the no-workspace
// case, so no per-workspace keying is needed.
import { getStored, setStored } from './storage';

const KEY = 'wallfacer-last-route';

export function rememberRoute(fullPath: string): void {
  setStored(KEY, fullPath);
}

// The currently stored route, captured before any new navigation overwrites it.
export function storedRoute(): string | null {
  return getStored(KEY);
}

// Decide where a cold launch should land. We only override the default board
// landing (`/`): any explicit URL — a deep link, or a refresh on /plan — is
// honoured as-is. Returns the stored path to restore, or null to stay put.
export function routeToRestore(landedAt: string, stored = getStored(KEY)): string | null {
  if (landedAt !== '/') return null;          // explicit URL — honour it
  if (!stored || stored === '/') return null; // nothing better to restore
  return stored;
}
