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
  ['implementation', 'testing'].forEach(function(t) {
    const btn = document.getElementById('left-tab-' + t);
    const panel = document.getElementById('left-panel-' + t);
    const active = t === tab;
    if (btn) btn.classList.toggle('active', active);
    if (panel) panel.classList.toggle('hidden', !active);
  });
  if (getOpenModalTaskId()) {
    history.replaceState(null, '', '#' + getOpenModalTaskId() + '/' + tab);
  }
}

// --- Timeline Gantt chart ---

// Phase colors per spec: worktree_setup → muted blue, agent_turn → accent, commit → green.
var _phaseColors = {
  worktree_setup: '#5e81ac',
  agent_turn:     'var(--accent)',
  commit:         '#3fb950',
  container_run:  '#9e6ec7',
  refinement:     '#d19a66',
};

function _phaseColor(phase) {
  return _phaseColors[phase] || 'var(--text-muted)';
}

// Convert a raw span phase+label pair into a human-readable display label.
function _humanSpanLabel(phase, label) {
  var m;
  if (phase === 'agent_turn') {
    if ((m = label.match(/^implementation_(\d+)$/))) return 'Impl. Turn ' + m[1];
    if ((m = label.match(/^test_(\d+)$/))) return 'Test Turn ' + m[1];
    if ((m = label.match(/^agent_turn_(\d+)$/))) return 'Turn ' + m[1]; // legacy
    return label;
  }
  if (phase === 'container_run') {
    var actMap = {
      'implementation':  'Container (Impl.)',
      'test':            'Container (Test)',
      'commit_message':  'Container (Commit)',
      'oversight':       'Container (Oversight)',
      'oversight_test':  'Container (Oversight-Test)',
      'refinement':      'Container (Refine)',
      'title':           'Container (Title)',
      'idea_agent':      'Container (Ideas)',
      'container_run':   'Container', // legacy
    };
    return actMap[label] || ('Container (' + label + ')');
  }
  if (phase === 'worktree_setup') return 'Worktree Setup';
  if (phase === 'commit') return 'Commit & Push';
  if (phase === 'refinement') return 'Refinement';
  return label || phase;
}

// Inject keyframe + tooltip CSS once into <head>.
function _ensureTimelineStyles() {
  if (document.getElementById('tl-css')) return;
  var s = document.createElement('style');
  s.id = 'tl-css';
  s.textContent =
    '@keyframes tl-stripe{0%{background-position:0 0}100%{background-position:22.6px 0}}' +
    '.tl-bar{cursor:default;transition:opacity .12s}.tl-bar:hover{opacity:.75}' +
    '#tl-tip{position:fixed;z-index:9999;pointer-events:none;' +
      'background:var(--bg-card);border:1px solid var(--border);' +
      'border-radius:6px;padding:6px 10px;font-size:11px;line-height:1.65;' +
      'white-space:pre;box-shadow:0 2px 10px rgba(0,0,0,.28);display:none;' +
      'color:var(--text-primary,#cdd6f4);}';
  document.head.appendChild(s);
  var tip = document.createElement('div');
  tip.id = 'tl-tip';
  document.body.appendChild(tip);
}

// Attach mousemove/leave listeners to .tl-bar elements inside container.
function _attachTimelineTips(container) {
  var tip = document.getElementById('tl-tip');
  if (!tip) return;
  container.querySelectorAll('.tl-bar[data-tip]').forEach(function(bar) {
    bar.addEventListener('mousemove', function(e) {
      tip.textContent = bar.dataset.tip;
      tip.style.display = 'block';
      var tx = e.clientX + 12;
      var ty = e.clientY - tip.offsetHeight - 6;
      if (ty < 4) ty = e.clientY + 16;
      tip.style.left = tx + 'px';
      tip.style.top = ty + 'px';
    });
    bar.addEventListener('mouseleave', function() {
      tip.style.display = 'none';
    });
  });
}

// Format a millisecond duration as human-readable string.
function _fmtMs(ms) {
  if (ms < 1000) return ms + 'ms';
  if (ms < 60000) return (ms / 1000).toFixed(1) + 's';
  var m = Math.floor(ms / 60000);
  var s = Math.round((ms % 60000) / 1000);
  return m + 'm\u202f' + s + 's';
}

// True when a span has no valid end time (Go zero time or missing).
function _spanIsOpen(span) {
  if (!span.ended_at) return true;
  var ms = new Date(span.ended_at).getTime();
  return isNaN(ms) || ms <= 0; // pre-epoch = Go zero time (0001-01-01)
}

// Build the HTML for the Gantt chart from a spans array.
function _buildTimelineHtml(spans) {
  if (!spans || spans.length === 0) {
    return '<div style="padding:12px 0;font-size:12px;color:var(--text-muted);">No timing data yet.</div>';
  }

  var LABEL_W = 130; // px for label column
  var ROW_H   = 24;  // px per row
  var now = Date.now();

  // Time bounds
  var t0 = Infinity;
  spans.forEach(function(s) {
    var ts = new Date(s.started_at).getTime();
    if (ts < t0) t0 = ts;
  });
  var tEnd = t0;
  spans.forEach(function(s) {
    var te = _spanIsOpen(s) ? now : new Date(s.ended_at).getTime();
    if (te > tEnd) tEnd = te;
  });
  var totalMs = Math.max(tEnd - t0, 1000);

  // Build time map for gap compression
  var tmSpans = spans.map(function(s) {
    var ts = new Date(s.started_at).getTime();
    var te = _spanIsOpen(s) ? now : new Date(s.ended_at).getTime();
    return { startMs: ts, endMs: te };
  });
  var timeMap = buildTimeMap(tmSpans, t0, tEnd);

  // Pick a tick interval that keeps at most ~8 ticks so labels don't overlap.
  var niceIntervals = [1000, 2000, 5000, 10000, 15000, 30000, 60000, 120000, 300000, 600000, 1800000, 3600000];
  var TICK_MS = niceIntervals[niceIntervals.length - 1];
  for (var ni = 0; ni < niceIntervals.length; ni++) {
    if (totalMs / niceIntervals[ni] <= 8) { TICK_MS = niceIntervals[ni]; break; }
  }

  function fmtTickLabel(ms) {
    if (ms === 0) return '0s';
    if (ms < 60000) return (ms / 1000) + 's';
    var m = Math.floor(ms / 60000);
    var s = (ms % 60000) / 1000;
    return s === 0 ? m + 'm' : m + 'm' + s + 's';
  }

  // X-axis tick marks and grid lines
  var ticksHtml = '';
  if (timeMap.compressed) {
    // Compressed mode: place ticks at visual positions, label with real time
    var tickFracs = [0, 0.25, 0.5, 0.75, 1];
    tickFracs.forEach(function(f) {
      var x = (f * 100);
      var realMs = timeMap.fromPercent(f * 100) - t0;
      var lbl = fmtTickLabel(Math.round(realMs));
      ticksHtml +=
        '<div style="position:absolute;left:' + x.toFixed(2) + '%;transform:translateX(-50%);bottom:2px;' +
          'font-size:10px;color:var(--text-muted);white-space:nowrap;user-select:none;">' + lbl + '</div>' +
        '<div style="position:absolute;left:' + x.toFixed(2) + '%;top:0;bottom:0;' +
          'border-left:1px dashed var(--border);opacity:.45;pointer-events:none;"></div>';
    });
    // Add hatched break indicators for compressed gaps (axis only, no label here)
    timeMap.segments.forEach(function(seg) {
      if (!seg.compressed) return;
      var gapLeft = timeMap.toPercent(seg.start);
      var gapRight = timeMap.toPercent(seg.end);
      var gapWidth = gapRight - gapLeft;
      if (gapWidth < 0.1) return;
      var gapDur = _fmtMs(seg.end - seg.start);
      var gapStartLabel = _fmtMs(seg.start - t0);
      var gapEndLabel = _fmtMs(seg.end - t0);
      var tipText = 'Idle ' + gapDur + '\n' + gapStartLabel + ' \u2192 ' + gapEndLabel;
      ticksHtml +=
        '<div title="' + escapeHtml(tipText) + '" style="' +
          'position:absolute;left:' + gapLeft.toFixed(2) + '%;width:' + gapWidth.toFixed(2) + '%;' +
          'top:0;bottom:0;' +
          'background:repeating-linear-gradient(120deg,transparent,transparent 3px,var(--border) 3px,var(--border) 4px);' +
          'opacity:0.3;pointer-events:none;"></div>';
    });
  } else {
    for (var tickMs = 0; tickMs <= totalMs + TICK_MS / 2; tickMs += TICK_MS) {
      var x = Math.min((tickMs / totalMs) * 100, 100);
      var lbl = fmtTickLabel(tickMs);
      ticksHtml +=
        '<div style="position:absolute;left:' + x + '%;transform:translateX(-50%);bottom:2px;' +
          'font-size:10px;color:var(--text-muted);white-space:nowrap;user-select:none;">' + lbl + '</div>' +
        '<div style="position:absolute;left:' + x + '%;top:0;bottom:0;' +
          'border-left:1px dashed var(--border);opacity:.45;pointer-events:none;"></div>';
    }
  }

  // Pre-build per-row gap stripe HTML (no label — labels go in overlay)
  var rowGapHtml = '';
  if (timeMap.compressed) {
    timeMap.segments.forEach(function(seg) {
      if (!seg.compressed) return;
      var gapLeft = timeMap.toPercent(seg.start);
      var gapRight = timeMap.toPercent(seg.end);
      var gapWidth = gapRight - gapLeft;
      if (gapWidth < 0.1) return;
      var gapDur = _fmtMs(seg.end - seg.start);
      var gapStartLabel = _fmtMs(seg.start - t0);
      var gapEndLabel = _fmtMs(seg.end - t0);
      var tipText = 'Idle ' + gapDur + '\n' + gapStartLabel + ' \u2192 ' + gapEndLabel;
      rowGapHtml +=
        '<div title="' + escapeHtml(tipText) + '" style="' +
          'position:absolute;left:' + gapLeft.toFixed(2) + '%;width:' + gapWidth.toFixed(2) + '%;' +
          'top:0;bottom:0;' +
          'background:repeating-linear-gradient(120deg,transparent,transparent 3px,var(--border) 3px,var(--border) 4px);' +
          'opacity:0.15;pointer-events:none;"></div>';
    });
  }

  // Span rows
  var rowsHtml = '';
  spans.forEach(function(span) {
    var ts   = new Date(span.started_at).getTime();
    var open = _spanIsOpen(span);
    var te   = open ? now : new Date(span.ended_at).getTime();
    var dur  = te - ts;

    var leftPct  = timeMap.toPercent(ts);
    var rightPct = timeMap.toPercent(te);
    var left  = leftPct;
    var width = Math.max(rightPct - leftPct, 0.5);
    var color = _phaseColor(span.phase);
    var durStr   = _fmtMs(dur);
    var relStart = '+' + _fmtMs(ts - t0);
    var absStart = new Date(ts).toLocaleTimeString([], {hour: '2-digit', minute: '2-digit', second: '2-digit'});

    var humanLabel = _humanSpanLabel(span.phase, span.label);

    // Tooltip text (newlines preserved via white-space:pre on the tooltip div)
    var tipText =
      humanLabel + '\n' +
      'Phase:    ' + span.phase + '\n' +
      'Label:    ' + span.label + '\n' +
      'Start:    ' + relStart + ' (' + absStart + ')\n' +
      'Duration: ' + durStr + (open ? ' (running\u2026)' : '');

    var barLabel = humanLabel + ' \xb7 ' + durStr + (open ? '\u2026' : '');

    var barStyle =
      'position:absolute;left:' + left + '%;width:' + width + '%;height:16px;top:4px;' +
      'border-radius:3px;overflow:hidden;display:flex;align-items:center;' +
      'padding:0 4px;box-sizing:border-box;';
    if (open) {
      // Striped animated bar for unclosed (running) spans
      barStyle +=
        'background-color:' + color + ';' +
        'background-image:repeating-linear-gradient(45deg,transparent,transparent 8px,' +
          'rgba(255,255,255,.18) 8px,rgba(255,255,255,.18) 16px);' +
        'background-size:22.6px 22.6px;animation:tl-stripe .6s linear infinite;';
    } else {
      barStyle += 'background:' + color + ';';
    }

    rowsHtml +=
      '<div style="display:flex;align-items:center;height:' + ROW_H + 'px;">' +
        '<div style="width:' + LABEL_W + 'px;flex-shrink:0;font-size:11px;color:var(--text-muted);' +
          'overflow:hidden;text-overflow:ellipsis;white-space:nowrap;' +
          'padding-right:6px;text-align:right;" title="' + escapeHtml(span.phase + ':' + span.label) + '">' +
          escapeHtml(humanLabel) +
        '</div>' +
        '<div style="flex:1;position:relative;height:' + ROW_H + 'px;">' +
          rowGapHtml +
          '<div class="tl-bar" data-tip="' + escapeHtml(tipText) + '" style="' + barStyle + '">' +
            '<span style="font-size:10px;color:#fff;text-shadow:0 0 3px rgba(0,0,0,.55);' +
              'overflow:hidden;text-overflow:ellipsis;white-space:nowrap;min-width:0;pointer-events:none;">' +
              escapeHtml(barLabel) +
            '</span>' +
          '</div>' +
        '</div>' +
      '</div>';
  });

  // Phase legend (unique phases present)
  var _phaseLegendNames = {
    worktree_setup: 'Worktree Setup',
    agent_turn:     'Agent Turn',
    commit:         'Commit & Push',
    container_run:  'Container',
    refinement:     'Refinement',
  };
  var seenPhases = {};
  var legendHtml = '';
  spans.forEach(function(s) {
    if (!seenPhases[s.phase]) {
      seenPhases[s.phase] = true;
      var phaseName = _phaseLegendNames[s.phase] || s.phase;
      legendHtml +=
        '<span style="display:inline-flex;align-items:center;gap:4px;font-size:11px;' +
          'color:var(--text-muted);margin-right:10px;">' +
          '<span style="width:10px;height:10px;border-radius:2px;flex-shrink:0;display:inline-block;' +
            'background:' + _phaseColor(s.phase) + ';"></span>' +
          escapeHtml(phaseName) +
        '</span>';
    }
  });

  // Build gap label overlay (single set of labels spanning the full rows area)
  var gapLabelOverlay = '';
  if (timeMap.compressed) {
    var rowsH = spans.length * ROW_H;
    timeMap.segments.forEach(function(seg) {
      if (!seg.compressed) return;
      var gapLeft = timeMap.toPercent(seg.start);
      var gapRight = timeMap.toPercent(seg.end);
      var gapWidth = gapRight - gapLeft;
      if (gapWidth < 0.1) return;
      var gapDur = _fmtMs(seg.end - seg.start);
      var gapStartLabel = _fmtMs(seg.start - t0);
      var gapEndLabel = _fmtMs(seg.end - t0);
      var tipText = 'Idle ' + gapDur + '\n' + gapStartLabel + ' \u2192 ' + gapEndLabel;
      gapLabelOverlay +=
        '<span title="' + escapeHtml(tipText) + '" style="' +
          'position:absolute;left:' + gapLeft.toFixed(2) + '%;width:' + gapWidth.toFixed(2) + '%;' +
          'top:0;height:' + rowsH + 'px;' +
          'display:flex;align-items:center;justify-content:center;' +
          'font-size:8px;color:var(--text-muted);' +
          'overflow:hidden;white-space:nowrap;pointer-events:none;' +
          'writing-mode:vertical-rl;text-orientation:mixed;">' +
          escapeHtml(gapDur) + '</span>';
    });
  }

  return (
    '<div style="overflow-x:auto;padding:8px 0;">' +
      '<div style="min-width:500px;">' +
        // X-axis header row
        '<div style="display:flex;margin-bottom:4px;">' +
          '<div style="width:' + LABEL_W + 'px;flex-shrink:0;"></div>' +
          '<div style="flex:1;position:relative;height:22px;">' + ticksHtml + '</div>' +
        '</div>' +
        // Span rows with gap label overlay
        '<div style="position:relative;">' +
          rowsHtml +
          // Gap labels overlay (offset by LABEL_W to align with bar area)
          (gapLabelOverlay
            ? '<div style="position:absolute;top:0;left:' + LABEL_W + 'px;right:0;bottom:0;pointer-events:none;">' +
                gapLabelOverlay +
              '</div>'
            : '') +
        '</div>' +
        // Legend
        (legendHtml
          ? '<div style="border-top:1px solid var(--border);padding-top:6px;margin-top:6px;">' +
              legendHtml +
            '</div>'
          : '') +
      '</div>' +
    '</div>'
  );
}

// Start a 5-second polling loop to refresh the timeline while the task runs.
function _startTimelineRefresh(taskId) {
  _stopTimelineRefresh();
  if (typeof tasks === 'undefined' || typeof timelineRefreshTimer === 'undefined') return;
  var task = tasks.find(function(t) { return t.id === taskId; });
  if (!task || (task.status !== 'in_progress' && task.status !== 'committing')) return;
  timelineRefreshTimer = setInterval(function() {
    if (!currentTaskId || currentTaskId !== taskId) {
      _stopTimelineRefresh();
      return;
    }
    var t = tasks.find(function(tt) { return tt.id === taskId; });
    if (!t || (t.status !== 'in_progress' && t.status !== 'committing')) {
      _stopTimelineRefresh();
      renderTimeline(taskId); // one final refresh to show completed spans
      return;
    }
    renderTimeline(taskId);
  }, 5000);
}

// Stop any active timeline auto-refresh.
function _stopTimelineRefresh() {
  if (typeof timelineRefreshTimer !== 'undefined' && timelineRefreshTimer) {
    clearInterval(timelineRefreshTimer);
    timelineRefreshTimer = null;
  }
}

// Fetch spans and render the Gantt chart into #modal-timeline-chart.
function renderTimeline(taskId) {
  var seq = _modalState.seq;
  var el = document.getElementById('modal-timeline-chart');
  if (!el) return;
  // Only show loading placeholder on first load (avoid flicker on refresh)
  if (!el.dataset.loaded) {
    el.innerHTML = '<div style="font-size:12px;color:var(--text-muted);padding:8px 0;">Loading timeline\u2026</div>';
  }
  _ensureTimelineStyles();

  var signal = _modalState.abort ? _modalState.abort.signal : undefined;
  var req = (typeof apiGet === 'function')
    ? apiGet('/api/tasks/' + taskId + '/spans', { signal: signal })
    : fetch('/api/tasks/' + taskId + '/spans', { signal: signal }).then(function(res) { return res.json(); });
  req
    .then(function(data) {
      if (getOpenModalTaskId() !== taskId) return;
      if (_modalState.seq !== seq) return;
      var el2 = document.getElementById('modal-timeline-chart');
      if (!el2) return;
      el2.dataset.loaded = '1';
      var spans = (data && Array.isArray(data.spans)) ? data.spans : [];
      el2.innerHTML = _buildTimelineHtml(spans);
      _attachTimelineTips(el2);
    })
    .catch(function(err) {
      if (err && err.name === 'AbortError') return;
      if (getOpenModalTaskId() !== taskId) return;
      if (_modalState.seq !== seq) return;
      var el2 = document.getElementById('modal-timeline-chart');
      if (el2) el2.innerHTML = '<div style="font-size:12px;color:var(--text-muted);padding:8px 0;">Failed to load timeline.</div>';
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
    const anyVisible = ['implementation', 'testing'].some(function(t) {
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
  });
}
