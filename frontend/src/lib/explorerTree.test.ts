import { describe, it, expect } from 'vitest';
import { joinPath, mapEntries } from './explorerTree';

describe('joinPath', () => {
  it('joins parent + name with one separator', () => {
    expect(joinPath('/ws', 'a.txt')).toBe('/ws/a.txt');
  });
  it('collapses trailing slashes on the parent', () => {
    expect(joinPath('/ws/', 'a.txt')).toBe('/ws/a.txt');
    expect(joinPath('/ws///', 'a.txt')).toBe('/ws/a.txt');
  });
});

describe('mapEntries', () => {
  it('returns [] for null/undefined', () => {
    expect(mapEntries('/ws', null)).toEqual([]);
    expect(mapEntries('/ws', undefined)).toEqual([]);
  });

  it('reconstructs path + is_dir from raw {name,type} (backend omits both)', () => {
    const out = mapEntries('/ws', [
      { name: 'main.go', type: 'file', size: 10 },
      { name: 'internal', type: 'dir' },
    ]);
    // dir sorts before file
    expect(out.map((e) => e.name)).toEqual(['internal', 'main.go']);
    const dir = out.find((e) => e.name === 'internal')!;
    expect(dir.is_dir).toBe(true);
    expect(dir.path).toBe('/ws/internal');
    const file = out.find((e) => e.name === 'main.go')!;
    expect(file.is_dir).toBe(false);
    expect(file.path).toBe('/ws/main.go');
    expect(file.size).toBe(10);
  });

  it('sorts dirs first then case-insensitive alphabetical', () => {
    const out = mapEntries('/ws', [
      { name: 'Zebra.txt', type: 'file' },
      { name: 'alpha.txt', type: 'file' },
      { name: 'Beta', type: 'dir' },
      { name: 'apex', type: 'dir' },
    ]);
    expect(out.map((e) => e.name)).toEqual(['apex', 'Beta', 'alpha.txt', 'Zebra.txt']);
  });

  it('builds nested child paths from a subdirectory request path', () => {
    const out = mapEntries('/ws/internal', [{ name: 'handler.go', type: 'file' }]);
    expect(out[0].path).toBe('/ws/internal/handler.go');
  });

  it('defaults missing size to 0', () => {
    const out = mapEntries('/ws', [{ name: 'd', type: 'dir' }]);
    expect(out[0].size).toBe(0);
  });
});
