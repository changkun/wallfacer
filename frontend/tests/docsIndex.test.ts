// The cloud docs index (src/data/docs.ts) is generated from the Reading
// Order in docs/guide/usage.md, which the Go server also parses for the
// local-mode sidebar. This test fails when the two drift: re-run
// `node frontend/scripts/gen-docs-index.mjs` after editing usage.md.
import { describe, expect, it } from 'vitest';
import { readFileSync, readdirSync } from 'node:fs';
import { resolve } from 'node:path';
// eslint-disable-next-line import/no-relative-packages
import { parseReadingOrder, renderDocsTs } from '../scripts/gen-docs-index.mjs';

const root = resolve(process.cwd(), '..');
const usageMd = readFileSync(resolve(root, 'docs/guide/usage.md'), 'utf8');

describe('docs index generation', () => {
  it('checked-in docs.ts matches what the generator produces from usage.md', () => {
    const generated = renderDocsTs(parseReadingOrder(usageMd));
    const checkedIn = readFileSync(resolve(process.cwd(), 'src/data/docs.ts'), 'utf8');
    expect(checkedIn).toBe(generated);
  });

  it('every reading-order slug resolves to a guide file, and vice versa', () => {
    const slugs = parseReadingOrder(usageMd).map((e) => e.slug);
    expect(slugs.length).toBeGreaterThanOrEqual(10);
    const files = readdirSync(resolve(root, 'docs/guide'))
      .filter((f) => f.endsWith('.md'))
      .map((f) => f.replace(/\.md$/, ''));
    for (const slug of slugs) {
      expect(files, `reading order links ${slug}.md`).toContain(slug);
    }
    for (const file of files) {
      if (file === 'usage') continue; // the index document itself
      expect(slugs, `docs/guide/${file}.md is orphaned from the reading order`).toContain(file);
    }
  });
});
