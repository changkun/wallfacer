import { describe, it, expect } from 'vitest';
import { renderMarkdown, renderMarkdownWithSourceLines } from './markdown';

describe('renderMarkdownWithSourceLines', () => {
  it('stamps the 1-based start line on top-level blocks', () => {
    const html = renderMarkdownWithSourceLines('# Title\n\nFirst para.\n\n## Heading');
    // First para is on source line 3, the ## heading on line 5.
    expect(html).toContain('data-source-line="3"');
    expect(html).toContain('data-source-line="5"');
  });

  it('stamps nested list items at their own line, not the list start', () => {
    // The bug: a level filter stamps only the <ul>, so a selection inside the
    // second item maps to line 1 instead of line 2.
    const html = renderMarkdownWithSourceLines('- one\n- two\n- three');
    // The list opens on line 1; the items are on lines 1, 2, 3.
    expect(html).toMatch(/<li[^>]*data-source-line="2"/);
    expect(html).toMatch(/<li[^>]*data-source-line="3"/);
  });

  it('leaves the default render path free of source-line attrs', () => {
    expect(renderMarkdown('# Title\n\n- a\n- b')).not.toContain('data-source-line');
  });
});

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
