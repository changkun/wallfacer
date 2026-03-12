// --- Usage Analytics (Stats) Modal ---

(function () {
  var modal, loadingEl, errorEl, contentEl;
  var setState;

  function init() {
    modal     = document.getElementById('stats-modal');
    loadingEl = document.getElementById('stats-loading');
    errorEl   = document.getElementById('stats-error');
    contentEl = document.getElementById('stats-content');
    bindModalBackdropClose(modal, closeStatsModal);
    setState = createModalStateController({
      loadingEl: loadingEl,
      errorEl: errorEl,
      contentEl: contentEl,
      contentState: 'content'
    });
  }

  function fmt(n) { return (n || 0).toLocaleString(); }
  function fmtCost(c) { return '$' + (c || 0).toFixed(4); }

  function fetchAndRender() {
    loadJsonEndpoint('/api/stats', renderStats, setState);
  }

  function renderSummary(data) {
    var el = document.getElementById('stats-summary');
    el.innerHTML =
      '<div style="display:flex;gap:24px;flex-wrap:wrap;padding:4px 0 20px;">' +
        '<div>' +
          '<div style="font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;margin-bottom:3px;">Total Cost</div>' +
          '<div style="font-size:22px;font-weight:600;">' + fmtCost(data.total_cost_usd) + '</div>' +
        '</div>' +
        '<div>' +
          '<div style="font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;margin-bottom:3px;">Input Tokens</div>' +
          '<div style="font-size:22px;font-weight:600;">' + fmt(data.total_input_tokens) + '</div>' +
        '</div>' +
        '<div>' +
          '<div style="font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;margin-bottom:3px;">Output Tokens</div>' +
          '<div style="font-size:22px;font-weight:600;">' + fmt(data.total_output_tokens) + '</div>' +
        '</div>' +
        '<div>' +
          '<div style="font-size:10px;color:var(--text-muted);text-transform:uppercase;letter-spacing:0.5px;margin-bottom:3px;">Cache Tokens</div>' +
          '<div style="font-size:22px;font-weight:600;">' + fmt(data.total_cache_tokens) + '</div>' +
        '</div>' +
      '</div>';
  }

  function appendRows(tbodyId, rows) {
    appendRowsToTbody(tbodyId, rows);
  }

  function renderByStatus(data) {
    var byStatus = data.by_status || {};
    var keys = Object.keys(byStatus).sort();
    var rows = keys.map(function (k) {
      var s = byStatus[k];
      return [
        { text: k,                     style: 'padding:6px 10px;font-weight:500;' },
        { text: fmt(s.input_tokens),   style: 'padding:6px 10px;text-align:right;color:var(--text-muted);' },
        { text: fmt(s.output_tokens),  style: 'padding:6px 10px;text-align:right;color:var(--text-muted);' },
        { text: fmtCost(s.cost_usd),   style: 'padding:6px 10px;text-align:right;font-weight:500;' }
      ];
    });
    appendRows('stats-by-status-tbody', rows);
  }

  var ACTIVITY_ORDER = ['implementation', 'test', 'refinement', 'title', 'oversight', 'oversight-test'];

  function renderByActivity(data) {
    var byActivity = data.by_activity || {};
    var seen = {};
    var keys = ACTIVITY_ORDER.filter(function (k) {
      if (byActivity[k]) { seen[k] = true; return true; }
      return false;
    });
    Object.keys(byActivity).sort().forEach(function (k) {
      if (!seen[k]) keys.push(k);
    });
    var rows = keys.map(function (k) {
      var a = byActivity[k];
      return [
        { text: k,                     style: 'padding:6px 10px;font-weight:500;' },
        { text: fmt(a.input_tokens),   style: 'padding:6px 10px;text-align:right;color:var(--text-muted);' },
        { text: fmt(a.output_tokens),  style: 'padding:6px 10px;text-align:right;color:var(--text-muted);' },
        { text: fmtCost(a.cost_usd),   style: 'padding:6px 10px;text-align:right;font-weight:500;' }
      ];
    });
    appendRows('stats-by-activity-tbody', rows);
  }

  function drawDailyChart(daily) {
    var canvas = document.getElementById('stats-daily-chart');
    if (!canvas || !canvas.getContext) return;
    var ctx = canvas.getContext('2d');
    var W = 600, H = 120;
    canvas.width = W;
    canvas.height = H;

    var padTop = 8, padBot = 24;
    var chartH = H - padTop - padBot;

    var maxCost = 0;
    daily.forEach(function (d) { if (d.cost_usd > maxCost) maxCost = d.cost_usd; });

    var today = new Date().toISOString().slice(0, 10);
    var barW = W / daily.length;
    var isDark = document.documentElement.getAttribute('data-theme') === 'dark';
    var barColor   = isDark ? '#475569' : '#94a3b8';
    var todayColor = '#3b82f6';
    var labelColor = isDark ? '#64748b' : '#94a3b8';

    ctx.clearRect(0, 0, W, H);

    daily.forEach(function (d, i) {
      var bh = (maxCost > 0 && d.cost_usd > 0)
        ? Math.max(1, (d.cost_usd / maxCost) * chartH)
        : 0;
      var x = i * barW;

      if (bh > 0) {
        ctx.fillStyle = d.date === today ? todayColor : barColor;
        ctx.fillRect(x + 1, padTop + chartH - bh, barW - 2, bh);
      }

      if (i % 5 === 0) {
        var parts = d.date.split('-');
        var label = parts[1] + '-' + parts[2]; // MM-DD
        ctx.fillStyle = labelColor;
        ctx.font = '9px sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText(label, x + barW / 2, H - 6);
      }
    });
  }

  function renderByWorkspace(data) {
    var byWorkspace = data.by_workspace || {};
    var keys = Object.keys(byWorkspace);
    var section = document.getElementById('stats-by-workspace-section');
    if (!section) return;
    if (keys.length === 0) {
      section.style.display = 'none';
      return;
    }
    // Sort by cost descending.
    keys.sort(function (a, b) { return (byWorkspace[b].cost_usd || 0) - (byWorkspace[a].cost_usd || 0); });
    var rows = keys.map(function (path) {
      var w = byWorkspace[path];
      // Use last path component as display label; full path in tooltip.
      var parts = path.replace(/\\/g, '/').split('/');
      var label = parts[parts.length - 1] || path;
      return [
        { html: '<span title="' + escapeHtml(path) + '" style="cursor:default;">' + escapeHtml(label) + '</span>',
          style: 'padding:6px 10px;font-weight:500;' },
        { text: fmt(w.count),          style: 'padding:6px 10px;text-align:right;color:var(--text-muted);' },
        { text: fmt(w.input_tokens),   style: 'padding:6px 10px;text-align:right;color:var(--text-muted);' },
        { text: fmt(w.output_tokens),  style: 'padding:6px 10px;text-align:right;color:var(--text-muted);' },
        { text: fmtCost(w.cost_usd),   style: 'padding:6px 10px;text-align:right;font-weight:500;' }
      ];
    });
    appendRows('stats-by-workspace-tbody', rows);
    section.style.display = '';
  }

  function renderTopTasks(data) {
    var tasks = data.top_tasks || [];
    var rows = tasks.map(function (t) {
      return [
        {
          html: '<a href="#" style="color:var(--accent);text-decoration:none;display:block;max-width:360px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" ' +
                'onclick="event.preventDefault();closeStatsModal();setTimeout(function(){openModal(' + JSON.stringify(t.id) + ')},50);">' +
                escapeHtml(t.title) + '</a>',
          style: 'padding:6px 10px;max-width:360px;'
        },
        { text: t.status,           style: 'padding:6px 10px;color:var(--text-muted);white-space:nowrap;' },
        { text: fmtCost(t.cost_usd), style: 'padding:6px 10px;text-align:right;font-weight:500;white-space:nowrap;' }
      ];
    });
    appendRows('stats-top-tasks-tbody', rows);
  }

  function renderStats(data) {
    renderSummary(data);
    renderByStatus(data);
    renderByActivity(data);
    renderByWorkspace(data);
    drawDailyChart(data.daily_usage || []);
    renderTopTasks(data);
    setState('content');
  }

  window.openStatsModal = function () {
    openModalPanel(modal);
    fetchAndRender();
  };

  window.closeStatsModal = function () {
    closeModalPanel(modal);
  };

  document.addEventListener('DOMContentLoaded', init);
})();
