import { describe, it, expect } from 'vitest';
import { renderMarkdown } from './markdown';

describe('renderMarkdown doc images', () => {
  it('emits a light/dark pair for doc-relative images, rewritten to the asset route', () => {
    const html = renderMarkdown('![The board](images/board.png)', 'guide');
    // Light variant: original path under /api/docs-asset/<baseDir>/.
    expect(html).toContain(
      '<img class="doc-shot__img doc-shot__img--light" src="/api/docs-asset/guide/images/board.png"',
    );
    // Dark variant: -dark suffix before the extension.
    expect(html).toContain(
      '<img class="doc-shot__img doc-shot__img--dark" src="/api/docs-asset/guide/images/board-dark.png"',
    );
    // Alt text is preserved on both.
    expect(html.match(/alt="The board"/g)).toHaveLength(2);
    expect(html).toContain('<span class="doc-shot">');
  });

  it('handles a nested category baseDir and a leading ./', () => {
    const html = renderMarkdown('![x](./shots/a.webp)', 'guide/sub');
    expect(html).toContain('src="/api/docs-asset/guide/sub/shots/a.webp"');
    expect(html).toContain('src="/api/docs-asset/guide/sub/shots/a-dark.webp"');
  });

  it('leaves external and absolute images untouched (no asset rewrite, single img)', () => {
    const ext = renderMarkdown('![e](https://example.com/a.png)', 'guide');
    expect(ext).toContain('src="https://example.com/a.png"');
    expect(ext).not.toContain('/api/docs-asset');
    expect(ext).not.toContain('doc-shot__img--dark');

    const abs = renderMarkdown('![a](/static/x.png)', 'guide');
    expect(abs).toContain('src="/static/x.png"');
    expect(abs).not.toContain('/api/docs-asset');
  });
});
