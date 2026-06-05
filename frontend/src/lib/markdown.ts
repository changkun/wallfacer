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

export function stripFirstHeading(src: string): string {
  const lines = src.split('\n');
  if (lines.length > 0 && lines[0].startsWith('# ')) {
    return lines.slice(1).join('\n');
  }
  return src;
}
