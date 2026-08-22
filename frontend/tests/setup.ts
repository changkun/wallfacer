// Vitest global setup.
//
// Node exposes an experimental `localStorage` global that is an empty object
// unless `--localstorage-file` is passed, and it shadows the Storage that
// happy-dom installs on the window. The result is a `localStorage` that exists
// but whose methods are all undefined, so src/lib/storage.ts feature-detects
// its way to a no-op and every persistence assertion passes vacuously.
//
// Install a spec-compliant, in-memory Storage so persistence tests exercise the
// real read/write path. Each test file gets a fresh module registry, so the
// backing map is per-file; tests that need per-case isolation still clear it.

class MemoryStorage implements Storage {
  private map = new Map<string, string>();

  get length(): number {
    return this.map.size;
  }

  key(index: number): string | null {
    return [...this.map.keys()][index] ?? null;
  }

  getItem(key: string): string | null {
    return this.map.get(String(key)) ?? null;
  }

  setItem(key: string, value: string): void {
    this.map.set(String(key), String(value));
  }

  removeItem(key: string): void {
    this.map.delete(String(key));
  }

  clear(): void {
    this.map.clear();
  }
}

for (const target of [globalThis, globalThis.window].filter(Boolean)) {
  Object.defineProperty(target, 'localStorage', {
    value: new MemoryStorage(),
    configurable: true,
    writable: true,
  });
}
