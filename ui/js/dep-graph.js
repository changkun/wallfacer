// Dependency graph overlay — draws bezier-curve arrows between cards
// that have depends_on relationships.
//
// Colours convey dependency status:
//   #22c55e  — dependency is done (solid line)
//   #ef4444  — dependency failed (dashed)
//   #f59e0b  — any other status (dashed)

function hideDependencyGraph() {
  const svg = document.getElementById('dep-graph-overlay');
  if (svg) svg.remove();
  _detachColumnScrollListeners();
}

// Redraw on scroll within any board column, throttled to one redraw per frame.
let _depGraphScrollPending = false;
function _onColumnScroll() {
  if (_depGraphScrollPending) return;
  _depGraphScrollPending = true;
  requestAnimationFrame(() => {
    _depGraphScrollPending = false;
    if (window.depGraphEnabled && typeof tasks !== 'undefined') renderDependencyGraph(tasks);
  });
}

function _attachColumnScrollListeners() {
  document.querySelectorAll('.column').forEach(col => {
    col.addEventListener('scroll', _onColumnScroll, { passive: true });
  });
}

function _detachColumnScrollListeners() {
  document.querySelectorAll('.column').forEach(col => {
    col.removeEventListener('scroll', _onColumnScroll);
  });
}

function renderDependencyGraph(tasks) {
  hideDependencyGraph();

  // Build edge list: each entry is { from: taskId, to: depId, depStatus }
  const edges = [];
  for (const t of tasks) {
    if (!t.depends_on || t.depends_on.length === 0) continue;
    for (const depId of t.depends_on) {
      const dep = tasks.find(d => d.id === depId);
      if (!dep) continue;
      edges.push({ from: t.id, to: depId, depStatus: dep.status });
    }
  }
  if (edges.length === 0) return;

  _attachColumnScrollListeners();

  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.id = 'dep-graph-overlay';
  svg.style.cssText = 'position:fixed;top:0;left:0;width:100vw;height:100vh;pointer-events:none;z-index:40;overflow:visible;';
  document.body.appendChild(svg);

  // Clip drawing to the board area so curves don't bleed through the header
  // or other UI chrome when cards scroll out of view.
  const boardEl = document.getElementById('board');
  const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs');
  const clipPath = document.createElementNS('http://www.w3.org/2000/svg', 'clipPath');
  clipPath.id = 'dep-graph-clip';
  const clipRect = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
  if (boardEl) {
    const br = boardEl.getBoundingClientRect();
    clipRect.setAttribute('x', br.left);
    clipRect.setAttribute('y', br.top);
    clipRect.setAttribute('width', br.width);
    clipRect.setAttribute('height', br.height);
  } else {
    clipRect.setAttribute('x', 0);
    clipRect.setAttribute('y', 0);
    clipRect.setAttribute('width', '100vw');
    clipRect.setAttribute('height', '100vh');
  }
  clipPath.appendChild(clipRect);
  defs.appendChild(clipPath);
  svg.appendChild(defs);

  const g = document.createElementNS('http://www.w3.org/2000/svg', 'g');
  g.setAttribute('clip-path', 'url(#dep-graph-clip)');
  svg.appendChild(g);

  for (const { from, to, depStatus } of edges) {
    const fromEl = document.querySelector('[data-task-id="' + from + '"]');
    const toEl = document.querySelector('[data-task-id="' + to + '"]');
    if (!fromEl || !toEl) continue;

    const fr = fromEl.getBoundingClientRect();
    const tr = toEl.getBoundingClientRect();

    // Arrow starts at the top-centre of the dependent card (from)
    // and ends at the bottom-centre of the dependency card (to).
    const x1 = fr.left + fr.width / 2;
    const y1 = fr.top;
    const x2 = tr.left + tr.width / 2;
    const y2 = tr.bottom;
    const cy = (y1 + y2) / 2;

    const color = depStatus === 'done' ? '#22c55e'
      : depStatus === 'failed' ? '#ef4444'
      : '#f59e0b';

    const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    path.setAttribute('d', `M${x1},${y1} C${x1},${cy} ${x2},${cy} ${x2},${y2}`);
    path.setAttribute('stroke', color);
    path.setAttribute('stroke-width', '2');
    path.setAttribute('fill', 'none');
    path.setAttribute('stroke-dasharray', depStatus === 'done' ? 'none' : '6,3');

    // Small circle at the arrowhead (end of the dependency)
    const marker = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
    marker.setAttribute('cx', x2);
    marker.setAttribute('cy', y2);
    marker.setAttribute('r', '4');
    marker.setAttribute('fill', color);

    g.appendChild(path);
    g.appendChild(marker);
  }
}

function toggleDependencyGraph() {
  const cb = document.getElementById('dep-graph-toggle');
  window.depGraphEnabled = cb ? cb.checked : !window.depGraphEnabled;
  if (typeof render === 'function') render();
}

// Expose via window so that onclick handlers and render.js can call them.
window.hideDependencyGraph = hideDependencyGraph;
window.renderDependencyGraph = renderDependencyGraph;
window.toggleDependencyGraph = toggleDependencyGraph;

// Redraw on window resize (debounced) so arrows track moved cards.
let _depGraphResizeTimer;
window.addEventListener('resize', () => {
  clearTimeout(_depGraphResizeTimer);
  _depGraphResizeTimer = setTimeout(() => {
    if (window.depGraphEnabled) renderDependencyGraph(tasks);
  }, 100);
});
