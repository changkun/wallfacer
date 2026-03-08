// --- Multi-turn result rendering ---

function detectResultType(text) {
  if (!text) return 'result';
  const planPatterns = [
    /^#{1,3}\s+.*\bplan\b/im,
    /^#{1,3}\s+.*\bphase\s*\d/im,
    /^#{1,3}\s+.*\bstep\s*\d/im,
    /\bimplementation plan\b/i,
    /^#{1,3}\s+.*\bapproach\b/im,
    /^#{1,3}\s+.*\bproposal\b/im,
    /^#{1,3}\s+.*\bdesign\b/im,
    /^#{1,3}\s+.*\barchitecture\b/im,
    /^#{1,3}\s+.*\bstrategy\b/im,
  ];
  return planPatterns.some(function(p) { return p.test(text); }) ? 'plan' : 'result';
}

function copyResultEntry(entryId) {
  const rawEl = document.getElementById(entryId + '-raw');
  if (!rawEl) return;
  const text = rawEl.textContent;
  const btn = event.currentTarget;
  navigator.clipboard.writeText(text).then(function() {
    const origHTML = btn.innerHTML;
    btn.textContent = 'Copied!';
    setTimeout(function() { btn.innerHTML = origHTML; }, 1500);
  }).catch(function() {});
}

function toggleResultEntryRaw(entryId) {
  const renderedEl = document.getElementById(entryId + '-rendered');
  const rawEl = document.getElementById(entryId + '-raw');
  const btn = event.currentTarget;
  const showingRaw = !rawEl.classList.contains('hidden');
  if (showingRaw) {
    renderedEl.classList.remove('hidden');
    rawEl.classList.add('hidden');
    btn.textContent = 'Raw';
  } else {
    renderedEl.classList.add('hidden');
    rawEl.classList.remove('hidden');
    btn.textContent = 'Preview';
  }
}

const _copyIcon = '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>';

function setLeftTab(tab) {
  ['implementation', 'testing', 'timeline'].forEach(function(t) {
    const btn = document.getElementById('left-tab-' + t);
    const panel = document.getElementById('left-panel-' + t);
    const active = t === tab;
    if (btn) btn.classList.toggle('active', active);
    if (panel) panel.classList.toggle('hidden', !active);
  });
  if (tab === 'timeline' && typeof currentTaskId !== 'undefined' && currentTaskId) {
    renderTimeline(currentTaskId);
  }
}

// Phase color map for the timeline bar chart.
var _phaseColors = {
  worktree_setup: '#3fb950',
  agent_turn:     '#79c0ff',
  container_run:  '#d2a8ff',
  commit:         '#d97757',
};

function _phaseColor(phase) {
  return _phaseColors[phase] || '#6e7681';
}

// renderTimeline fetches span data for taskId and renders a proportional
// horizontal bar chart into #modal-timeline-chart using CSS flexbox.
function renderTimeline(taskId) {
  var el = document.getElementById('modal-timeline-chart');
  if (!el) return;
  el.innerHTML = '<div style="font-size:12px;color:var(--text-secondary);padding:8px 0;">Loading timeline\u2026</div>';

  fetch('/api/tasks/' + taskId + '/spans')
    .then(function(res) { return res.json(); })
    .then(function(spans) {
      if (!spans || spans.length === 0) {
        el.innerHTML = '<div style="font-size:12px;color:var(--text-secondary);padding:8px 0;">No timing data available.</div>';
        return;
      }

      // Compute total elapsed from first start to last end for proportional sizing.
      var minStart = new Date(spans[0].started_at).getTime();
      var maxEnd = 0;
      spans.forEach(function(s) {
        var end = new Date(s.ended_at).getTime();
        if (end > maxEnd) maxEnd = end;
      });
      var totalMs = maxEnd - minStart || 1;

      var rows = spans.map(function(s) {
        var startMs = new Date(s.started_at).getTime() - minStart;
        var leftPct = (startMs / totalMs * 100).toFixed(2);
        var widthPct = Math.max((s.duration_ms / totalMs * 100), 0.5).toFixed(2);
        var color = _phaseColor(s.phase);
        var durLabel = s.duration_ms >= 1000
          ? (s.duration_ms / 1000).toFixed(1) + 's'
          : s.duration_ms + 'ms';
        var tooltip = escapeHtml(s.label + ': ' + s.duration_ms + 'ms');
        return '<div style="display:flex;align-items:center;gap:8px;margin-bottom:5px;">' +
          '<div style="width:110px;font-size:11px;color:var(--text-secondary);text-align:right;flex-shrink:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" title="' + escapeHtml(s.label) + '">' + escapeHtml(s.label) + '</div>' +
          '<div style="flex:1;position:relative;height:14px;background:var(--bg-card);border-radius:2px;">' +
            '<div style="position:absolute;left:' + leftPct + '%;width:' + widthPct + '%;height:100%;background:' + color + ';border-radius:2px;" title="' + tooltip + '"></div>' +
          '</div>' +
          '<div style="width:44px;font-size:11px;color:var(--text-secondary);text-align:right;flex-shrink:0;font-family:monospace;">' + escapeHtml(durLabel) + '</div>' +
        '</div>';
      }).join('');

      // Build legend from unique phases present.
      var seen = {};
      var legendItems = [];
      spans.forEach(function(s) {
        if (!seen[s.phase]) {
          seen[s.phase] = true;
          legendItems.push(
            '<span style="display:inline-flex;align-items:center;gap:4px;font-size:11px;color:var(--text-secondary);margin-right:10px;">' +
            '<span style="display:inline-block;width:10px;height:10px;border-radius:2px;background:' + _phaseColor(s.phase) + ';flex-shrink:0;"></span>' +
            escapeHtml(s.phase) + '</span>'
          );
        }
      });

      var el2 = document.getElementById('modal-timeline-chart');
      if (!el2) return;
      el2.innerHTML =
        '<div style="padding:8px 0;">' +
          '<div style="margin-bottom:8px;">' + rows + '</div>' +
          '<div style="border-top:1px solid var(--border);padding-top:6px;">' + legendItems.join('') + '</div>' +
        '</div>';
    })
    .catch(function() {
      var el2 = document.getElementById('modal-timeline-chart');
      if (el2) el2.innerHTML = '<div style="font-size:12px;color:var(--text-secondary);padding:8px 0;">Failed to load timeline.</div>';
    });
}

function renderResultsFromEvents(results, opts) {
  opts = opts || {};
  const tabId = opts.tabId || 'left-tab-implementation';
  const listId = opts.listId || 'modal-results-list';
  const entryPrefix = opts.entryPrefix || 'result-entry-';

  const summarySection = document.getElementById('modal-summary-section');
  const tabBtn = document.getElementById(tabId);
  const listEl = document.getElementById(listId);
  if (!results || results.length === 0) {
    if (tabBtn) tabBtn.classList.add('hidden');
    const anyVisible = ['implementation', 'testing', 'timeline'].some(function(t) {
      const btn = document.getElementById('left-tab-' + t);
      return btn && !btn.classList.contains('hidden');
    });
    if (!anyVisible && summarySection) summarySection.classList.add('hidden');
    return;
  }

  if (tabBtn) tabBtn.classList.remove('hidden');
  if (summarySection) summarySection.classList.remove('hidden');
  if (opts.autoSwitch) setLeftTab(tabId.replace('left-tab-', ''));

  const totalTurns = results.length;
  // Display newest turn first so the most recent result is immediately visible.
  listEl.innerHTML = [...results].reverse().map(function(result, i) {
    const isNewest = i === 0;
    const originalIndex = totalTurns - 1 - i; // chronological index (0-based)
    const isPlan = detectResultType(result) === 'plan';
    const entryId = entryPrefix + originalIndex;

    const typeBadgeHtml = isPlan
      ? '<span class="result-type-badge result-type-plan">Plan</span>'
      : '';
    const turnLabelHtml = totalTurns > 1
      ? '<span class="result-turn-label">Turn ' + (originalIndex + 1) + '</span>'
      : '';
    const labelsHtml = '<div class="result-entry-labels">' + typeBadgeHtml + turnLabelHtml + '</div>';
    const btnRowHtml =
      '<div class="flex items-center gap-1.5">' +
        '<button onclick="copyResultEntry(\'' + entryId + '\')" class="btn-icon" title="Copy">' +
          _copyIcon + ' Copy' +
        '</button>' +
        '<button onclick="toggleResultEntryRaw(\'' + entryId + '\')" class="btn-icon">Raw</button>' +
      '</div>';
    const bodyHtml =
      '<div id="' + entryId + '-rendered" class="result-entry-body prose-content">' + renderMarkdown(result) + '</div>' +
      '<pre id="' + entryId + '-raw" class="result-entry-body hidden">' + escapeHtml(result) + '</pre>';

    if (!isNewest) {
      return '<details class="result-entry">' +
        '<summary class="result-entry-summary">' + labelsHtml + '</summary>' +
        '<div class="result-entry-actions">' + btnRowHtml + '</div>' +
        bodyHtml +
        '</details>';
    } else {
      return '<div class="result-entry">' +
        '<div class="result-entry-header">' + labelsHtml + btnRowHtml + '</div>' +
        bodyHtml +
        '</div>';
    }
  }).join('');
}

function renderTestResultsFromEvents(results) {
  renderResultsFromEvents(results, {
    tabId: 'left-tab-testing',
    listId: 'modal-test-results-list',
    entryPrefix: 'test-result-entry-',
    autoSwitch: results && results.length > 0,
  });
}
