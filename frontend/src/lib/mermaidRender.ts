// Mermaid post-processor for markdown content. Mirrors the legacy
// ui/js/lib/markdown-render.js behaviour:
//   - Lazy-load mermaid v11 (already a dependency).
//   - Read CSS custom properties so diagrams adopt the active theme.
//   - Render `.mermaid-block[data-mermaid]` placeholders emitted by our
//     markdown-it fence renderer.
//   - Fix node-label contrast when authors specify light fills via
//     `style fill:#xxx`.
//   - Click on a diagram opens a fullscreen overlay with zoom + pan +
//     keyboard navigation. Esc closes; +/- zoom; arrows pan.
//   - Re-render diagrams when `<html data-theme>` changes.

let mermaidPromise: Promise<typeof import('mermaid').default> | null = null;
let renderSeq = 0;

async function loadMermaid() {
  if (!mermaidPromise) {
    mermaidPromise = import('mermaid').then((m) => {
      const lib = m.default;
      lib.initialize({ startOnLoad: false, securityLevel: 'loose', ...themeConfig() });
      return lib;
    });
  }
  return mermaidPromise;
}

function cssVar(name: string): string {
  if (typeof document === 'undefined') return '';
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function themeConfig() {
  return {
    theme: 'base' as const,
    themeVariables: {
      primaryColor: cssVar('--bg-input') || cssVar('--bg-sunk'),
      primaryTextColor: cssVar('--text') || cssVar('--ink'),
      primaryBorderColor: cssVar('--border') || cssVar('--rule'),
      lineColor: cssVar('--text-muted') || cssVar('--ink-3'),
      secondaryColor: cssVar('--bg-card'),
      tertiaryColor: cssVar('--bg-raised') || cssVar('--bg-sunk'),
      background: cssVar('--bg-card'),
      mainBkg: cssVar('--bg-input') || cssVar('--bg-sunk'),
      nodeBorder: cssVar('--border') || cssVar('--rule'),
      clusterBkg: cssVar('--bg-card'),
      clusterBorder: cssVar('--border') || cssVar('--rule'),
      titleColor: cssVar('--text') || cssVar('--ink'),
      edgeLabelBackground: cssVar('--bg-card'),
      nodeTextColor: cssVar('--text') || cssVar('--ink'),
      actorTextColor: cssVar('--text') || cssVar('--ink'),
      actorBkg: cssVar('--bg-input') || cssVar('--bg-sunk'),
      actorBorder: cssVar('--border') || cssVar('--rule'),
      signalColor: cssVar('--text') || cssVar('--ink'),
      signalTextColor: cssVar('--text') || cssVar('--ink'),
      labelBoxBkgColor: cssVar('--bg-input') || cssVar('--bg-sunk'),
      labelBoxBorderColor: cssVar('--border') || cssVar('--rule'),
      labelTextColor: cssVar('--text') || cssVar('--ink'),
      loopTextColor: cssVar('--text') || cssVar('--ink'),
      noteBkgColor: cssVar('--bg-input') || cssVar('--bg-sunk'),
      noteTextColor: cssVar('--text') || cssVar('--ink'),
      noteBorderColor: cssVar('--border') || cssVar('--rule'),
      activationBkgColor: cssVar('--bg-input') || cssVar('--bg-sunk'),
      activationBorderColor: cssVar('--border') || cssVar('--rule'),
      sequenceNumberColor: cssVar('--text') || cssVar('--ink'),
      fontFamily: 'inherit',
      fontSize: '13px',
    },
  };
}

function hexLuminance(hex: string): number {
  let h = hex.replace('#', '');
  if (h.length === 3) h = h[0] + h[0] + h[1] + h[1] + h[2] + h[2];
  if (h.length < 6) return -1;
  const channels = [
    Number.parseInt(h.slice(0, 2), 16) / 255,
    Number.parseInt(h.slice(2, 4), 16) / 255,
    Number.parseInt(h.slice(4, 6), 16) / 255,
  ];
  const lin = channels.map((c) => (c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4)));
  return 0.2126 * lin[0] + 0.7152 * lin[1] + 0.0722 * lin[2];
}

function fixNodeContrast(container: Element) {
  const shapes = container.querySelectorAll<SVGElement>(
    'svg .node rect, svg .node polygon, svg .node circle',
  );
  for (const shape of shapes) {
    const styleAttr = shape.getAttribute('style') ?? '';
    const match = styleAttr.match(/fill:\s*(#[0-9a-fA-F]{3,8})/);
    if (!match) continue;
    const lum = hexLuminance(match[1]);
    if (lum < 0) continue;
    const node = shape.closest('.node');
    if (!node) continue;
    const labels = node.querySelectorAll<HTMLElement>('.nodeLabel, foreignObject span');
    const colour = lum > 0.5 ? '#1a1a1a' : '#f0f0f0';
    for (const label of labels) label.style.color = colour;
  }
}

export async function enhanceMermaid(container: HTMLElement | null): Promise<void> {
  if (!container) return;
  const blocks = container.querySelectorAll<HTMLElement>('.mermaid-block:not(.mermaid-rendered)');
  if (blocks.length === 0) return;
  const mermaid = await loadMermaid();

  for (const block of blocks) {
    const code = block.getAttribute('data-mermaid');
    if (!code) continue;
    const id = 'mermaid-diagram-' + Date.now() + '-' + ++renderSeq;
    try {
      const { svg } = await mermaid.render(id, code);
      const div = document.createElement('div');
      div.className = 'mermaid-diagram';
      div.title = 'Click to expand';
      div.innerHTML = svg;
      div.addEventListener('click', () => expandDiagram(div));
      block.innerHTML = '';
      block.appendChild(div);
      block.classList.add('mermaid-rendered');
      fixNodeContrast(div);
    } catch (err) {
      console.error('mermaid render error:', err);
      // Leave the source code visible (the .mermaid-src <pre> already inside).
    }
  }
}

// Re-initialise mermaid against the current CSS variables and re-render
// every diagram in the document. Called when `<html data-theme>` changes.
export async function reinitMermaidTheme(): Promise<void> {
  if (!mermaidPromise) return;
  const mermaid = await mermaidPromise;
  mermaid.initialize({ startOnLoad: false, securityLevel: 'loose', ...themeConfig() });
  if (typeof document === 'undefined') return;
  // Reset all rendered blocks back to placeholders so enhanceMermaid runs again.
  const rendered = document.querySelectorAll<HTMLElement>('.mermaid-block.mermaid-rendered');
  for (const block of rendered) {
    const code = block.getAttribute('data-mermaid') ?? '';
    block.classList.remove('mermaid-rendered');
    block.innerHTML =
      '<pre class="mermaid-src"><code>' +
      code.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;') +
      '</code></pre>';
  }
  // Re-enhance any container that has placeholders. Walking the whole
  // document is OK — enhanceMermaid is a no-op when nothing matches.
  await enhanceMermaid(document.body);
}

let themeObserver: MutationObserver | null = null;
export function watchThemeReinit() {
  if (themeObserver || typeof document === 'undefined') return;
  themeObserver = new MutationObserver(() => {
    void reinitMermaidTheme();
  });
  themeObserver.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['data-theme'],
  });
}

// ── Click-to-expand overlay ────────────────────────────────────────

function expandDiagram(sourceDiv: HTMLElement) {
  const svg = sourceDiv.querySelector('svg');
  if (!svg) return;

  const overlay = document.createElement('div');
  overlay.className = 'diagram-overlay';

  const viewport = document.createElement('div');
  viewport.className = 'diagram-overlay__viewport';

  const surface = document.createElement('div');
  surface.className = 'diagram-overlay__surface';
  const clone = svg.cloneNode(true) as SVGElement;
  const vb = clone.getAttribute('viewBox');
  if (vb) {
    const parts = vb.split(/[\s,]+/);
    const w = Number.parseFloat(parts[2]) || 800;
    const h = Number.parseFloat(parts[3]) || 600;
    clone.setAttribute('width', String(w));
    clone.setAttribute('height', String(h));
  }
  clone.removeAttribute('style');
  surface.appendChild(clone);
  viewport.appendChild(surface);

  const toolbar = document.createElement('div');
  toolbar.className = 'diagram-overlay__toolbar';
  toolbar.innerHTML =
    '<button type="button" title="Zoom in">+</button>' +
    '<button type="button" title="Zoom out">&minus;</button>' +
    '<button type="button" title="Reset view">Fit</button>' +
    '<span class="diagram-overlay__hint">Scroll to zoom · drag to pan · Esc to close</span>' +
    '<button type="button" title="Close">&times;</button>';

  overlay.appendChild(viewport);
  overlay.appendChild(toolbar);

  let scale = 1;
  let tx = 0;
  let ty = 0;
  let dragging = false;
  let dragStartX = 0;
  let dragStartY = 0;
  let txStart = 0;
  let tyStart = 0;

  function applyTransform() {
    surface.style.transform = `translate(${tx}px,${ty}px) scale(${scale})`;
  }

  function zoomTo(newScale: number, cx: number, cy: number) {
    const ratio = newScale / scale;
    tx = cx - ratio * (cx - tx);
    ty = cy - ratio * (cy - ty);
    scale = newScale;
    applyTransform();
  }

  function resetView() {
    scale = 1;
    tx = 0;
    ty = 0;
    applyTransform();
  }

  const btns = toolbar.querySelectorAll('button');
  btns[0].addEventListener('click', () => zoomTo(scale * 1.3, viewport.clientWidth / 2, viewport.clientHeight / 2));
  btns[1].addEventListener('click', () => zoomTo(scale / 1.3, viewport.clientWidth / 2, viewport.clientHeight / 2));
  btns[2].addEventListener('click', resetView);
  btns[3].addEventListener('click', removeOverlay);

  viewport.addEventListener('wheel', (e) => {
    e.preventDefault();
    const rect = viewport.getBoundingClientRect();
    const cx = e.clientX - rect.left;
    const cy = e.clientY - rect.top;
    const factor = e.deltaY < 0 ? 1.15 : 1 / 1.15;
    zoomTo(Math.max(0.1, Math.min(10, scale * factor)), cx, cy);
  }, { passive: false });

  viewport.addEventListener('mousedown', (e) => {
    if (e.button !== 0) return;
    dragging = true;
    dragStartX = e.clientX;
    dragStartY = e.clientY;
    txStart = tx;
    tyStart = ty;
    viewport.style.cursor = 'grabbing';
    e.preventDefault();
  });

  function onMouseMove(e: MouseEvent) {
    if (!dragging) return;
    tx = txStart + (e.clientX - dragStartX);
    ty = tyStart + (e.clientY - dragStartY);
    applyTransform();
  }
  function onMouseUp() {
    if (!dragging) return;
    dragging = false;
    viewport.style.cursor = '';
  }
  window.addEventListener('mousemove', onMouseMove);
  window.addEventListener('mouseup', onMouseUp);

  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.stopImmediatePropagation();
      removeOverlay();
      return;
    }
    const cx = viewport.clientWidth / 2;
    const cy = viewport.clientHeight / 2;
    if (e.key === '=' || e.key === '+') { zoomTo(scale * 1.3, cx, cy); e.preventDefault(); }
    else if (e.key === '-') { zoomTo(scale / 1.3, cx, cy); e.preventDefault(); }
    else if (e.key === '0') { resetView(); e.preventDefault(); }
    else if (e.key === 'ArrowLeft') { tx += 50; applyTransform(); e.preventDefault(); }
    else if (e.key === 'ArrowRight') { tx -= 50; applyTransform(); e.preventDefault(); }
    else if (e.key === 'ArrowUp') { ty += 50; applyTransform(); e.preventDefault(); }
    else if (e.key === 'ArrowDown') { ty -= 50; applyTransform(); e.preventDefault(); }
  }

  function removeOverlay() {
    overlay.remove();
    document.removeEventListener('keydown', onKey, true);
    window.removeEventListener('mousemove', onMouseMove);
    window.removeEventListener('mouseup', onMouseUp);
  }

  document.addEventListener('keydown', onKey, true);
  document.body.appendChild(overlay);

  requestAnimationFrame(() => {
    const svgRect = clone.getBoundingClientRect();
    const vpRect = viewport.getBoundingClientRect();
    if (svgRect.width > 0 && svgRect.height > 0) {
      const fit =
        Math.min(vpRect.width / svgRect.width, vpRect.height / svgRect.height, 2) * 0.9;
      scale = fit;
      tx = (vpRect.width - svgRect.width * fit) / 2;
      ty = (vpRect.height - svgRect.height * fit) / 2;
      applyTransform();
    }
  });
}
