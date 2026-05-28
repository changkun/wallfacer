import { describe, expect, it } from 'vitest';
import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const root = process.cwd();
const index = readFileSync(resolve(root, 'index.html'), 'utf8');
const fontsCss = readFileSync(resolve(root, 'src/styles/fonts.css'), 'utf8');

describe('font loading', () => {
  it('preloads the critical Latin fonts so first paint avoids a FOUT swap', () => {
    for (const href of [
      '/fonts/inter-400.woff2',
      '/fonts/inter-600.woff2',
      '/fonts/instrument-serif-regular.woff2',
      '/fonts/instrument-serif-italic.woff2',
    ]) {
      expect(index).toContain(`<link rel="preload" href="${href}" as="font" type="font/woff2" crossorigin />`);
    }
  });

  it('serves Latin faces as WOFF2 with font-display: block', () => {
    expect(fontsCss).toContain("url('/fonts/inter-400.woff2') format('woff2')");
    expect(fontsCss).toContain("url('/fonts/instrument-serif-regular.woff2') format('woff2')");
    expect(fontsCss).toContain('font-display: block;');
    expect(fontsCss).not.toContain("format('truetype')");
  });

  it('keeps the CJK face as a full-coverage WOFF2 (no glyph-dropping subset)', () => {
    expect(fontsCss).toContain("url('/fonts/lxgw-wenkai-tc-400.woff2') format('woff2')");
    expect(fontsCss).not.toContain('lxgw-wenkai-tc-400-subset');
    expect(existsSync(resolve(root, 'public/fonts/lxgw-wenkai-tc-400.woff2'))).toBe(true);
  });

  it('keeps heavy TTF runtime fonts out of public assets', () => {
    for (const filename of [
      'inter-400.ttf',
      'inter-500.ttf',
      'inter-600.ttf',
      'inter-700.ttf',
      'instrument-serif-regular.ttf',
      'instrument-serif-italic.ttf',
      'lxgw-wenkai-tc-400.ttf',
    ]) {
      expect(existsSync(resolve(root, 'public/fonts', filename)), filename).toBe(false);
    }
  });
});
