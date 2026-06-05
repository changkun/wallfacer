import { describe, expect, it } from 'vitest';
import { en } from './en';
import { zh } from './zh';

describe('i18n dictionaries', () => {
  it('en and zh have identical key sets', () => {
    const enKeys = Object.keys(en).sort();
    const zhKeys = Object.keys(zh).sort();
    const missingInZh = enKeys.filter(k => !(k in zh));
    const missingInEn = zhKeys.filter(k => !(k in en));
    expect(missingInZh, 'keys present in en but missing in zh').toEqual([]);
    expect(missingInEn, 'keys present in zh but missing in en').toEqual([]);
  });

  // Execution moved from containers to host processes; the marketing copy must
  // not reintroduce container-era language or non-existent CLI surfaces. Bare
  // "sandbox" stays allowed (harness abstraction, planning sandbox).
  it('contains no stale container-era or non-existent-command copy', () => {
    const banned = [/container/i, /podman/i, /docker/i, /sandbox image/i, /wallfacer exec\b/i, /容器/, /沙箱镜像/];
    for (const dict of [en, zh]) {
      for (const [key, value] of Object.entries(dict)) {
        for (const pattern of banned) {
          expect(pattern.test(value), `${key} = "${value}" matches banned ${pattern}`).toBe(false);
        }
      }
    }
  });
});
