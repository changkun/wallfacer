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
let _depGraphListenersAttached = false;
function _onColumnScroll() {
  if (_depGraphScrollPending) return;
  _depGraphScrollPending = true;
  requestAnimationFrame(() => {
    _depGraphScrollPending = false;
    if (window.depGraphEnabled && typeof tasks !== 'undefined') renderDependencyGraph(tasks);
  });
}

function _attachColumnScrollListeners() {
  if (_depGraphListenersAttached) return;
  document.querySelectorAll('.column').forEach(col => {
    col.addEventListener('scroll', _onColumnScroll, { passive: true });
  });
  _depGraphListenersAttached = true;
}

function _detachColumnScrollListeners() {
  if (!_depGraphListenersAttached) return;
  document.querySelectorAll('.column').forEach(col => {
    col.removeEventListener('scroll', _onColumnScroll);
  });
  _depGraphListenersAttached = false;
}

function _clearChildren(el) {
  if (!el || typeof el.replaceChildren !== 'function') return;
  el.replaceChildren();
}

function _ensureOverlay() {
  let svg = document.getElementById('dep-graph-overlay');
  let clipRect = null;
  let group = null;
  if (!svg) {
    svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.id = 'dep-graph-overlay';
    svg.style.cssText = 'position:fixed;top:0;left:0;width:100vw;height:100vh;pointer-events:none;z-index:40;overflow:visible;';

    const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs');
    const clipPath = document.createElementNS('http://www.w3.org/2000/svg', 'clipPath');
    clipPath.id = 'dep-graph-clip';
    clipRect = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
    clipPath.appendChild(clipRect);
    defs.appendChild(clipPath);
    svg.appendChild(defs);

    group = document.createElementNS('http://www.w3.org/2000/svg', 'g');
    group.setAttribute('clip-path', 'url(#dep-graph-clip)');
    svg.appendChild(group);
    document.body.appendChild(svg);
  } else {
    clipRect = svg.querySelector ? svg.querySelector('clipPath rect') : null;
    group = svg.querySelector ? svg.querySelector('g') : null;
  }
  return { svg, clipRect, group };
}

function _buildCardIndex(tasks) {
  const byId = new Map();
  if (document.querySelectorAll) {
    document.querySelectorAll('.card[data-task-id]').forEach(function(el) {
      if (el && el.dataset && el.dataset.taskId) byId.set(el.dataset.taskId, el);
    });
  }
  if (byId.size > 0) return byId;
  for (const task of tasks) {
    const el = document.querySelector('[data-task-id="' + task.id + '"]');
    if (el) byId.set(task.id, el);
  }
  return byId;
}

function renderDependencyGraph(tasks) {
  // Build edge list: each entry is { from: taskId, to: depId, depStatus }
  const taskById = new Map(tasks.map(function(task) { return [task.id, task]; }));
  const edges = [];
  for (const t of tasks) {
    if (!t.depends_on || t.depends_on.length === 0) continue;
    for (const depId of t.depends_on) {
      const dep = taskById.get(depId);
      if (!dep) continue;
      edges.push({ from: t.id, to: depId, depStatus: dep.status });
    }
  }
  if (edges.length === 0) {
    hideDependencyGraph();
    return;
  }

  _attachColumnScrollListeners();
  const overlay = _ensureOverlay();
  if (!overlay.group || !overlay.clipRect) return;
  _clearChildren(overlay.group);

  // Clip drawing to the board area so curves don't bleed through the header
  // or other UI chrome when cards scroll out of view.
  const boardEl = document.getElementById('board');
  if (boardEl) {
    const br = boardEl.getBoundingClientRect();
    overlay.clipRect.setAttribute('x', br.left);
    overlay.clipRect.setAttribute('y', br.top);
    overlay.clipRect.setAttribute('width', br.width);
    overlay.clipRect.setAttribute('height', br.height);
  } else {
    overlay.clipRect.setAttribute('x', 0);
    overlay.clipRect.setAttribute('y', 0);
    overlay.clipRect.setAttribute('width', '100vw');
    overlay.clipRect.setAttribute('height', '100vh');
  }
  const cardIndex = _buildCardIndex(tasks);

  for (const { from, to, depStatus } of edges) {
    const fromEl = cardIndex.get(from);
    const toEl = cardIndex.get(to);
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

    overlay.group.appendChild(path);
    overlay.group.appendChild(marker);
  }
}

function toggleDependencyGraph() {
  const cb = document.getElementById('dep-graph-toggle');
  window.depGraphEnabled = cb ? cb.checked : !window.depGraphEnabled;
  if (typeof updateAutomationActiveCount === 'function') updateAutomationActiveCount();
  if (typeof scheduleRender === 'function') scheduleRender();
  else if (typeof render === 'function') render();
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
