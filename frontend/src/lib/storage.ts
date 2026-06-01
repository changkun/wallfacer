// Safe localStorage access. During vite-ssg prerender the code runs in Node,
// where `localStorage` may be absent OR present as a partial shim whose
// methods are undefined — so `typeof localStorage !== 'undefined'` alone is
// not enough (calling .getItem throws "is not a function"). Every helper
// here feature-detects the method and swallows quota / privacy-mode errors.

function usable(): boolean {
  try {
    return typeof localStorage !== 'undefined' && typeof localStorage.getItem === 'function';
  } catch {
    return false;
  }
}

export function getStored(key: string): string | null {
  if (!usable()) return null;
  try { return localStorage.getItem(key); } catch { return null; }
}

export function setStored(key: string, value: string): void {
  if (!usable()) return;
  try { localStorage.setItem(key, value); } catch { /* quota / denied */ }
}

export function removeStored(key: string): void {
  if (!usable()) return;
  try { localStorage.removeItem(key); } catch { /* denied */ }
}
