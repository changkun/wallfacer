import MarkdownIt from 'markdown-it';
import anchor from 'markdown-it-anchor';

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

const md = new MarkdownIt({
  html: true,
  linkify: true,
  typographer: false,
}).use(anchor, {
  permalink: false,
  slugify: (s: string) =>
    s.toLowerCase().replace(/[^\w\s-]/g, '').replace(/\s+/g, '-').replace(/-+/g, '-').trim(),
});

// Source-line stamping (opt-in via env.sourceLines). When enabled, every block
// token that knows its source position gets a `data-source-line` attribute set
// to the 1-based line where that block starts (token.map[0] + 1). This matches
// the server's 1-based thread.line so the spec-comments layer can map both
// directions (selection to line on create, server line to element on placement)
// against the same convention. Default callers (docs, task prompts, chat) pass
// no flag and get unchanged output. This is the standard VS-Code-preview
// technique, scoped here so it never perturbs the shared render path.
md.core.ruler.push('source_line', (state) => {
  if (!(state.env as { sourceLines?: boolean })?.sourceLines) return;
  // state.tokens is the flat stream of every block token at every depth (only
  // inline content nests in .children), so stamping all non-inline tokens with
  // a source map reaches list items, blockquote contents, and table rows, not
  // just top-level blocks. Mapping a selection inside a list to the <li>'s own
  // line (not the list's start line) is what keeps create/placement accurate.
  // Closers have map === null and are skipped.
  for (const token of state.tokens) {
    if (token.map && token.type !== 'inline') {
      token.attrSet('data-source-line', String(token.map[0] + 1));
    }
  }
});

// Custom fence renderer: mermaid fences become a placeholder div consumed
// by enhanceMarkdown(); other fences get the standard <pre><code class="language-X">.
const defaultFence = md.renderer.rules.fence!;
md.renderer.rules.fence = (tokens, idx, options, env, self) => {
  const token = tokens[idx];
  const lang = (token.info ?? '').trim();
  if (lang === 'mermaid') {
    return (
      '<div class="mermaid-block" data-mermaid="' +
      escapeHtml(token.content) +
      '"><pre class="mermaid-src"><code>' +
      escapeHtml(token.content) +
      '</code></pre></div>\n'
    );
  }
  return defaultFence(tokens, idx, options, env, self);
};

// Image renderer: doc-relative images (e.g. ![](images/board.png)) are rewritten
// to the /api/docs-asset/<baseDir>/ route and emitted as a light+dark pair so
// screenshots track the active theme. The dark variant is the same path with a
// `-dark` suffix before the extension. External/absolute/data URLs render
// normally. baseDir is passed via env from the docs viewer (the doc's category
// dir, e.g. "guide").
const defaultImage = md.renderer.rules.image;
md.renderer.rules.image = (tokens, idx, options, env, self) => {
  const token = tokens[idx];
  const src = token.attrGet('src') ?? '';
  const external = /^(https?:)?\/\//.test(src) || src.startsWith('/') || src.startsWith('data:');
  if (external) {
    return defaultImage
      ? defaultImage(tokens, idx, options, env, self)
      : self.renderToken(tokens, idx, options);
  }
  const base = String((env as { baseDir?: string })?.baseDir ?? '').replace(/\/+$/, '');
  const rel = src.replace(/^\.?\//, '');
  const url = (p: string) => `/api/docs-asset/${base ? base + '/' : ''}${p}`;
  const dark = rel.replace(/(\.[a-z0-9]+)$/i, '-dark$1');
  const alt = escapeHtml(self.renderInlineAsText(token.children ?? [], options, env));
  return (
    '<span class="doc-shot">' +
    `<img class="doc-shot__img doc-shot__img--light" src="${url(rel)}" alt="${alt}" loading="lazy">` +
    `<img class="doc-shot__img doc-shot__img--dark" src="${url(dark)}" alt="${alt}" loading="lazy">` +
    '</span>'
  );
};

export function renderMarkdown(src: string, baseDir = ''): string {
  return md.render(src, { baseDir });
}

// renderMarkdownWithSourceLines renders with the source-line plugin enabled, so
// block elements carry `data-source-line`. Used by the spec viewer to map a
// text selection (and a server-resolved thread line) onto rendered DOM.
export function renderMarkdownWithSourceLines(src: string, baseDir = ''): string {
  return md.render(src, { baseDir, sourceLines: true });
}

export function stripFirstHeading(src: string): string {
  const lines = src.split('\n');
  if (lines.length > 0 && lines[0].startsWith('# ')) {
    return lines.slice(1).join('\n');
  }
  return src;
}
