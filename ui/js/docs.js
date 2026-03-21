// --- Documentation viewer ---

var _docsDismiss = null;
var _docsEntries = [];
var _docsCurrentSlug = '';
var _mermaidLoaded = false;
var _mermaidRenderSeq = 0;

function _ensureMermaid() {
  if (_mermaidLoaded) return Promise.resolve();
  return new Promise(function(resolve, reject) {
    var script = document.createElement('script');
    script.src = 'https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.min.js';
    script.onload = function() {
      if (typeof mermaid !== 'undefined') {
        var cs = getComputedStyle(document.documentElement);
        try { mermaid.initialize({
          startOnLoad: false,
          theme: 'base',
          themeVariables: {
            primaryColor: cs.getPropertyValue('--bg-input').trim(),
            primaryTextColor: cs.getPropertyValue('--text').trim(),
            primaryBorderColor: cs.getPropertyValue('--border').trim(),
            lineColor: cs.getPropertyValue('--text-muted').trim(),
            secondaryColor: cs.getPropertyValue('--bg-card').trim(),
            tertiaryColor: cs.getPropertyValue('--bg-raised').trim(),
            background: cs.getPropertyValue('--bg-card').trim(),
            mainBkg: cs.getPropertyValue('--bg-input').trim(),
            nodeBorder: cs.getPropertyValue('--border').trim(),
            clusterBkg: cs.getPropertyValue('--bg-card').trim(),
            clusterBorder: cs.getPropertyValue('--border').trim(),
            titleColor: cs.getPropertyValue('--text').trim(),
            edgeLabelBackground: cs.getPropertyValue('--bg-card').trim(),
            nodeTextColor: cs.getPropertyValue('--text').trim(),
            actorTextColor: cs.getPropertyValue('--text').trim(),
            actorBkg: cs.getPropertyValue('--bg-input').trim(),
            actorBorder: cs.getPropertyValue('--border').trim(),
            signalColor: cs.getPropertyValue('--text').trim(),
            signalTextColor: cs.getPropertyValue('--text').trim(),
            labelBoxBkgColor: cs.getPropertyValue('--bg-input').trim(),
            labelBoxBorderColor: cs.getPropertyValue('--border').trim(),
            labelTextColor: cs.getPropertyValue('--text').trim(),
            loopTextColor: cs.getPropertyValue('--text').trim(),
            noteBkgColor: cs.getPropertyValue('--bg-input').trim(),
            noteTextColor: cs.getPropertyValue('--text').trim(),
            noteBorderColor: cs.getPropertyValue('--border').trim(),
            activationBkgColor: cs.getPropertyValue('--bg-input').trim(),
            activationBorderColor: cs.getPropertyValue('--border').trim(),
            sequenceNumberColor: cs.getPropertyValue('--text').trim(),
            fontFamily: 'inherit',
            fontSize: '13px',
          }
        }); } catch(initErr) { console.error('mermaid init:', initErr); }
      }
      _mermaidLoaded = true;
      resolve();
    };
    script.onerror = function() { resolve(); };
    document.head.appendChild(script);
  });
}

async function _renderMermaidBlocks(container) {
  if (typeof mermaid === 'undefined') return;
  // Find mermaid blocks by multiple selectors to handle different marked versions.
  var blocks = container.querySelectorAll(
    'pre code.language-mermaid, pre code.mermaid, div.mermaid-raw'
  );
  if (!blocks.length) {
    // Fallback: scan all <code> elements for mermaid content heuristic.
    var allCodes = container.querySelectorAll('pre code');
    var mermaidCodes = [];
    for (var j = 0; j < allCodes.length; j++) {
      var text = allCodes[j].textContent.trim();
      if (/^(graph|flowchart|sequenceDiagram|stateDiagram|classDiagram|gantt|pie|erDiagram|journey)\b/.test(text)) {
        mermaidCodes.push(allCodes[j]);
      }
    }
    blocks = mermaidCodes;
  }
  for (var i = 0; i < blocks.length; i++) {
    var el = blocks[i];
    var pre = el.tagName === 'PRE' ? el : el.parentElement;
    var source = el.textContent;
    var id = 'mermaid-' + (++_mermaidRenderSeq);
    try {
      var result = await mermaid.render(id, source);
      var div = document.createElement('div');
      div.className = 'mermaid-diagram';
      div.innerHTML = result.svg;
      div.title = 'Click to expand';
      div.addEventListener('click', (function(d) {
        return function() { _expandDiagram(d); };
      })(div));
      pre.replaceWith(div);
    } catch (e) {
      // Leave the code block as-is on render failure.
    }
  }
}

function _expandDiagram(sourceDiv) {
  var svg = sourceDiv.querySelector('svg');
  if (!svg) return;

  var overlay = document.createElement('div');
  overlay.className = 'diagram-overlay';

  // Viewport that clips the pannable/zoomable content.
  var viewport = document.createElement('div');
  viewport.className = 'diagram-overlay__viewport';

  // Inner surface that receives transforms.
  var surface = document.createElement('div');
  surface.className = 'diagram-overlay__surface';
  var clone = svg.cloneNode(true);
  // Ensure the SVG has explicit dimensions from its viewBox so it
  // doesn't collapse to 0x0 inside the transform surface.
  var vb = clone.getAttribute('viewBox');
  if (vb) {
    var parts = vb.split(/[\s,]+/);
    var vbW = parseFloat(parts[2]) || 800;
    var vbH = parseFloat(parts[3]) || 600;
    clone.setAttribute('width', vbW);
    clone.setAttribute('height', vbH);
  }
  clone.removeAttribute('style');
  surface.appendChild(clone);
  viewport.appendChild(surface);

  // Toolbar.
  var toolbar = document.createElement('div');
  toolbar.className = 'diagram-overlay__toolbar';
  toolbar.innerHTML =
    '<button type="button" onclick="this._zoomIn()" title="Zoom in">+</button>' +
    '<button type="button" onclick="this._zoomOut()" title="Zoom out">&minus;</button>' +
    '<button type="button" onclick="this._reset()" title="Reset view">Fit</button>' +
    '<span class="diagram-overlay__hint">Scroll to zoom &middot; drag to pan</span>' +
    '<button type="button" onclick="this._close()" title="Close">&times;</button>';

  overlay.appendChild(viewport);
  overlay.appendChild(toolbar);

  // --- Pan & zoom state ---
  var scale = 1, tx = 0, ty = 0;
  var dragging = false, dragStartX = 0, dragStartY = 0, txStart = 0, tyStart = 0;

  function applyTransform() {
    surface.style.transform = 'translate(' + tx + 'px,' + ty + 'px) scale(' + scale + ')';
  }

  function zoomTo(newScale, cx, cy) {
    // Zoom toward (cx, cy) in viewport coordinates.
    var ratio = newScale / scale;
    tx = cx - ratio * (cx - tx);
    ty = cy - ratio * (cy - ty);
    scale = newScale;
    applyTransform();
  }

  function resetView() {
    scale = 1; tx = 0; ty = 0;
    applyTransform();
  }

  // Wire toolbar buttons.
  var btns = toolbar.querySelectorAll('button');
  btns[0]._zoomIn = function() { zoomTo(scale * 1.3, viewport.clientWidth / 2, viewport.clientHeight / 2); };
  btns[0].onclick = function() { btns[0]._zoomIn(); };
  btns[1]._zoomOut = function() { zoomTo(scale / 1.3, viewport.clientWidth / 2, viewport.clientHeight / 2); };
  btns[1].onclick = function() { btns[1]._zoomOut(); };
  btns[2]._reset = resetView;
  btns[2].onclick = resetView;
  btns[3]._close = removeOverlay;
  btns[3].onclick = removeOverlay;

  // Mouse wheel zoom.
  viewport.addEventListener('wheel', function(e) {
    e.preventDefault();
    var rect = viewport.getBoundingClientRect();
    var cx = e.clientX - rect.left;
    var cy = e.clientY - rect.top;
    var factor = e.deltaY < 0 ? 1.15 : 1 / 1.15;
    zoomTo(Math.max(0.1, Math.min(10, scale * factor)), cx, cy);
  }, { passive: false });

  // Mouse drag to pan.
  viewport.addEventListener('mousedown', function(e) {
    if (e.button !== 0) return;
    dragging = true;
    dragStartX = e.clientX; dragStartY = e.clientY;
    txStart = tx; tyStart = ty;
    viewport.style.cursor = 'grabbing';
    e.preventDefault();
  });
  window.addEventListener('mousemove', onMouseMove);
  window.addEventListener('mouseup', onMouseUp);
  function onMouseMove(e) {
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

  // Keyboard: +/- to zoom, arrow keys to pan.
  function onKey(e) {
    if (e.key === 'Escape') {
      e.stopImmediatePropagation();
      removeOverlay();
      return;
    }
    var cx = viewport.clientWidth / 2, cy = viewport.clientHeight / 2;
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
  // Start fitted: scale SVG to fill viewport.
  requestAnimationFrame(function() {
    var svgRect = clone.getBoundingClientRect();
    var vpRect = viewport.getBoundingClientRect();
    if (svgRect.width > 0 && svgRect.height > 0) {
      var fitScale = Math.min(vpRect.width / svgRect.width, vpRect.height / svgRect.height, 2) * 0.9;
      scale = fitScale;
      tx = (vpRect.width - svgRect.width * fitScale) / 2;
      ty = (vpRect.height - svgRect.height * fitScale) / 2;
      applyTransform();
    }
  });
}

async function openDocs(slug) {
  var modal = document.getElementById('docs-modal');
  if (!modal) return;
  modal.classList.remove('hidden');
  modal.style.display = 'flex';
  if (_docsDismiss) _docsDismiss();
  _docsDismiss = bindModalDismiss(modal, closeDocs);

  if (!_docsEntries.length) {
    try {
      _docsEntries = await api('/api/docs');
    } catch (e) {
      _docsEntries = [];
    }
  }
  renderDocsNav();
  var target = slug || (_docsEntries.length ? _docsEntries[0].slug : '');
  if (target) loadDoc(target);
}

function closeDocs() {
  var modal = document.getElementById('docs-modal');
  if (!modal) return;
  modal.classList.add('hidden');
  modal.style.display = '';
  if (_docsDismiss) { _docsDismiss(); _docsDismiss = null; }
}

function toggleDocsFullscreen() {
  var card = document.getElementById('docs-modal-card');
  if (!card) return;
  card.classList.toggle('docs-modal-card--fullscreen');
  // Also remove padding from overlay when fullscreen.
  var modal = document.getElementById('docs-modal');
  if (modal) {
    modal.style.padding = card.classList.contains('docs-modal-card--fullscreen') ? '0' : '';
  }
}

function renderDocsNav() {
  var nav = document.getElementById('docs-nav');
  if (!nav) return;
  var categories = {};
  _docsEntries.forEach(function(entry) {
    if (!categories[entry.category]) categories[entry.category] = [];
    categories[entry.category].push(entry);
  });
  var html = '';
  var catLabels = { guide: 'User Guide', internals: 'Technical Reference' };
  Object.keys(categories).forEach(function(cat) {
    html += '<div style="margin-bottom:12px;">';
    html += '<div style="font-size:10px;font-weight:700;text-transform:uppercase;letter-spacing:0.05em;color:var(--text-muted);margin-bottom:6px;">' + escapeHtml(catLabels[cat] || cat) + '</div>';
    categories[cat].forEach(function(entry) {
      var active = entry.slug === _docsCurrentSlug;
      html += '<button type="button" onclick="loadDoc(\'' + escapeHtml(entry.slug) + '\')" style="display:block;width:100%;text-align:left;padding:4px 8px;margin-bottom:2px;border:none;border-radius:4px;background:' + (active ? 'var(--bg-input)' : 'transparent') + ';color:' + (active ? 'var(--text-primary)' : 'inherit') + ';font-size:12px;cursor:pointer;font-weight:' + (active ? '600' : '400') + ';" onmouseover="this.style.background=\'var(--bg-input)\'" onmouseout="this.style.background=\'' + (active ? 'var(--bg-input)' : 'transparent') + '\'">' + escapeHtml(entry.title) + '</button>';
    });
    html += '</div>';
  });
  nav.innerHTML = html;
}

async function loadDoc(slug) {
  _docsCurrentSlug = slug;
  renderDocsNav();
  var content = document.getElementById('docs-content');
  if (!content) return;
  content.innerHTML = '<div style="color:var(--text-muted);font-size:12px;">Loading...</div>';
  try {
    var res = await fetch(withAuthToken('/api/docs/' + slug));
    if (!res.ok) throw new Error('Not found');
    var md = await res.text();
    content.innerHTML = renderMarkdown(md);
    content.scrollTop = 0;
    _rewriteDocLinks(content, slug);
    await _ensureMermaid();
    await _renderMermaidBlocks(content);
  } catch (e) {
    content.innerHTML = '<div style="color:var(--text-muted);">Failed to load document.</div>';
  }
}

// Rewrite relative .md links to navigate within the docs viewer.
function _rewriteDocLinks(container, currentSlug) {
  var links = container.querySelectorAll('a[href]');
  var currentDir = currentSlug.substring(0, currentSlug.lastIndexOf('/') + 1);
  for (var i = 0; i < links.length; i++) {
    var a = links[i];
    var href = a.getAttribute('href');
    if (!href || href.startsWith('http') || href.startsWith('#')) continue;
    if (!href.endsWith('.md') && !href.includes('.md#')) continue;
    // Resolve relative path against current doc directory.
    var resolved = _resolveDocPath(currentDir, href);
    // Strip .md extension and any anchor.
    var anchor = '';
    var hashIdx = resolved.indexOf('#');
    if (hashIdx !== -1) {
      anchor = resolved.substring(hashIdx);
      resolved = resolved.substring(0, hashIdx);
    }
    resolved = resolved.replace(/\.md$/, '');
    // Remove trailing / from directory references.
    resolved = resolved.replace(/\/$/, '');
    a.setAttribute('href', '#');
    a.setAttribute('data-doc-slug', resolved);
    a.onclick = (function(slug) {
      return function(e) { e.preventDefault(); loadDoc(slug); };
    })(resolved);
  }
}

function _resolveDocPath(base, rel) {
  // Handle ../ and ./ in relative paths.
  var parts = (base + rel).split('/');
  var resolved = [];
  for (var i = 0; i < parts.length; i++) {
    if (parts[i] === '..') { resolved.pop(); }
    else if (parts[i] !== '.' && parts[i] !== '') { resolved.push(parts[i]); }
  }
  return resolved.join('/');
}
