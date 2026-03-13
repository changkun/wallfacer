// --- Span Statistics Modal ---

(function () {
  var modal, loadingEl, errorEl, emptyEl, contentEl, summaryEl, tbody;
  var setState;

  // Human-readable metadata for known execution phases.
  var PHASE_INFO = {
    worktree_setup: {
      label: 'Worktree Setup',
      desc: 'Creates an isolated git worktree for the task'
    },
    agent_turn: {
      label: 'Agent Turn',
      desc: 'One execution turn of the Claude Code agent (start → stop_reason)'
    },
    container_run: {
      label: 'Container Run',
      desc: 'Full sandbox container lifecycle from start to exit'
    },
    commit: {
      label: 'Commit Pipeline',
      desc: 'Commits and pushes task changes to the git repository'
    }
  };

  function phaseLabel(phase) {
    return (PHASE_INFO[phase] && PHASE_INFO[phase].label) || phase;
  }

  function phaseDesc(phase) {
    return (PHASE_INFO[phase] && PHASE_INFO[phase].desc) || 'Custom execution phase';
  }

  // Returns an inline CSS color string based on duration thresholds.
  // Green < 5 s, amber 5–30 s, red ≥ 30 s.
  function colorStyleForMs(ms) {
    if (ms == null) return '';
    if (ms < 5000)  return 'color:#22863a;';
    if (ms < 30000) return 'color:#d97706;';
    return 'color:#dc2626;';
  }

  // Returns HTML for a compact bar showing p50 relative to the global max.
  function barHtml(p50Ms, globalMaxMs) {
    if (!globalMaxMs || p50Ms == null) return '';
    var pct = Math.min(100, Math.round((p50Ms / globalMaxMs) * 100));
    return (
      '<div style="background:var(--border);border-radius:2px;height:4px;' +
      'width:72px;margin-top:4px;overflow:hidden;">' +
        '<div style="background:var(--accent);height:100%;width:' + pct + '%;' +
        'border-radius:2px;"></div>' +
      '</div>'
    );
  }

  function init() {
    modal     = document.getElementById('span-stats-modal');
    loadingEl = document.getElementById('span-stats-loading');
    errorEl   = document.getElementById('span-stats-error');
    emptyEl   = document.getElementById('span-stats-empty');
    contentEl = document.getElementById('span-stats-content');
    summaryEl = document.getElementById('span-stats-summary');
    tbody     = document.getElementById('span-stats-tbody');

    bindModalBackdropClose(modal, closeSpanStats);
    setState = createModalStateController({
      loadingEl: loadingEl,
      errorEl: errorEl,
      emptyEl: emptyEl,
      contentEl: contentEl,
      contentState: 'table'
    });
  }

  function fetchStats() {
    loadJsonEndpoint('/api/debug/spans', renderStats, setState);
  }

  function renderStats(data) {
    var phases = data.phases || {};
    var keys = Object.keys(phases).sort();

    if (keys.length === 0) { setState('empty'); return; }

    // Compute the global max across all phases for proportional bar scaling.
    var globalMaxMs = 0;
    keys.forEach(function (k) {
      if (phases[k].max_ms > globalMaxMs) globalMaxMs = phases[k].max_ms;
    });

    summaryEl.innerHTML =
      '<strong>' + data.tasks_scanned + '</strong> tasks scanned &middot; ' +
      '<strong>' + data.spans_total + '</strong> spans across ' +
      '<strong>' + keys.length + '</strong> phase' + (keys.length === 1 ? '' : 's');

    tbody.innerHTML = '';
    keys.forEach(function (phase) {
      var s = phases[phase];
      var tr = createHoverRow([
        { html: '<div style="font-weight:500;font-size:12px;">' + escapeHtml(phaseLabel(phase)) + '</div>' +
               '<div style="font-size:11px;color:var(--text-muted);margin-top:2px;">' + escapeHtml(phaseDesc(phase)) + '</div>' },
        { text: s.count, style: 'padding:8px 10px;text-align:right;color:var(--text-muted);font-size:12px;' },
        { text: fmtMs(s.min_ms), style: 'padding:8px 10px;text-align:right;color:var(--text-muted);font-size:12px;' },
        { html: '<div style="font-weight:600;">' + fmtMs(s.p50_ms) + '</div>' + barHtml(s.p50_ms, globalMaxMs), style: 'padding:8px 10px;text-align:right;font-size:12px;' },
        { text: s.count > 0 ? (s.sum_ms / s.count).toFixed(0) + ' ms' : '\u2014', style: 'padding:8px 10px;text-align:right;font-size:12px;color:var(--text-muted);' },
        { text: fmtMs(s.p95_ms), style: 'padding:8px 10px;text-align:right;font-size:12px;font-weight:500;' + colorStyleForMs(s.p95_ms) },
        { text: fmtMs(s.p99_ms), style: 'padding:8px 10px;text-align:right;font-size:12px;' + colorStyleForMs(s.p99_ms) },
        { text: fmtMs(s.max_ms), style: 'padding:8px 10px;text-align:right;color:var(--text-muted);font-size:12px;' }
      ]);

      tbody.appendChild(tr);
    });
    setState('table');
  }

  function fmtMs(ms) {
    if (ms === undefined || ms === null) return '\u2014';
    if (ms < 1000) return ms + 'ms';
    return (ms / 1000).toFixed(1) + 's';
  }

  window.showSpanStats = function () {
    openModalPanel(modal);
    fetchStats();
  };

  window.closeSpanStats = function () {
    closeModalPanel(modal);
  };

  document.addEventListener('DOMContentLoaded', init);
})();
