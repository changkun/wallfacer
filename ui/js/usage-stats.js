// --- Usage Statistics Modal ---

(function () {
  var modal, loadingEl, errorEl, emptyEl, contentEl;
  var summaryEl, byStatusTbody, bySubAgentTbody, periodSelect;

  function init() {
    modal           = document.getElementById('usage-stats-modal');
    loadingEl       = document.getElementById('usage-stats-loading');
    errorEl         = document.getElementById('usage-stats-error');
    emptyEl         = document.getElementById('usage-stats-empty');
    contentEl       = document.getElementById('usage-stats-content');
    summaryEl       = document.getElementById('usage-stats-summary');
    byStatusTbody   = document.getElementById('usage-stats-by-status-tbody');
    bySubAgentTbody = document.getElementById('usage-stats-by-sub-agent-tbody');
    periodSelect    = document.getElementById('usage-stats-period');

    modal.addEventListener('click', function (e) {
      if (e.target === modal) closeUsageStats();
    });

    periodSelect.addEventListener('change', fetchStats);
  }

  function setState(state, msg) {
    loadingEl.style.display = state === 'loading' ? 'flex' : 'none';
    errorEl.classList.toggle('hidden',   state !== 'error');
    emptyEl.classList.toggle('hidden',   state !== 'empty');
    contentEl.classList.toggle('hidden', state !== 'content');
    if (state === 'error') errorEl.textContent = msg || 'Unknown error';
  }

  function fetchStats() {
    setState('loading');
    var days = periodSelect ? periodSelect.value : '7';
    fetch('/api/usage?days=' + encodeURIComponent(days))
      .then(function (res) {
        return res.json().then(function (data) { return { ok: res.ok, data: data }; });
      })
      .then(function (result) {
        if (!result.ok) { setState('error', result.data.error || JSON.stringify(result.data)); return; }
        renderStats(result.data);
      })
      .catch(function (err) { setState('error', String(err)); });
  }

  // Status badge colours (mirrors existing badge-* CSS classes via inline style).
  var statusColors = {
    done:        { bg: 'var(--badge-done-bg)',        fg: 'var(--badge-done-fg)' },
    failed:      { bg: 'var(--badge-failed-bg)',      fg: 'var(--badge-failed-fg)' },
    cancelled:   { bg: 'var(--badge-cancelled-bg)',   fg: 'var(--badge-cancelled-fg)' },
    in_progress: { bg: 'var(--badge-inprogress-bg)',  fg: 'var(--badge-inprogress-fg)' },
    waiting:     { bg: 'var(--badge-waiting-bg)',     fg: 'var(--badge-waiting-fg)' },
    backlog:     { bg: 'var(--badge-backlog-bg)',     fg: 'var(--badge-backlog-fg)' },
    committing:  { bg: 'var(--badge-committing-bg)',  fg: 'var(--badge-committing-fg)' },
  };

  function statusBadge(status) {
    var c = statusColors[status] || { bg: 'var(--bg-raised)', fg: 'var(--text-muted)' };
    return '<span style="display:inline-block;padding:1px 7px;border-radius:999px;font-size:11px;font-weight:600;background:' +
      c.bg + ';color:' + c.fg + ';">' + escapeHtml(status.replace('_', '\u00a0')) + '</span>';
  }

  var agentLabels = {
    implementation: 'Implementation',
    test:           'Test',
    refinement:     'Refinement',
    title:          'Title gen.',
    oversight:      'Oversight',
    'oversight-test': 'Oversight (test)',
  };

  function agentLabel(key) {
    return agentLabels[key] || escapeHtml(key);
  }

  function fmtTokens(n) {
    if (!n) return '\u2014';
    return n.toLocaleString();
  }

  function fmtCost(usd) {
    if (!usd) return '\u2014';
    return '$' + usd.toFixed(4);
  }

  function usageRow(label, usage, isBold) {
    var tr = document.createElement('tr');
    tr.style.cssText = 'border-bottom: 1px solid var(--border); transition: background 0.1s;';
    tr.addEventListener('mouseenter', function () { tr.style.background = 'var(--bg-raised)'; });
    tr.addEventListener('mouseleave', function () { tr.style.background = ''; });
    var totalTokens = (usage.input_tokens || 0) + (usage.output_tokens || 0);
    tr.innerHTML =
      '<td style="padding:6px 10px;' + (isBold ? 'font-weight:600;' : '') + '">' + label + '</td>' +
      '<td style="padding:6px 10px;text-align:right;color:var(--text-muted);">' + fmtTokens(usage.input_tokens) + '</td>' +
      '<td style="padding:6px 10px;text-align:right;color:var(--text-muted);">' + fmtTokens(usage.output_tokens) + '</td>' +
      '<td style="padding:6px 10px;text-align:right;color:var(--text-muted);">' + fmtTokens(totalTokens || 0) + '</td>' +
      '<td style="padding:6px 10px;text-align:right;font-weight:600;color:var(--accent);">' + fmtCost(usage.cost_usd) + '</td>';
    return tr;
  }

  function renderStats(data) {
    var total      = data.total      || {};
    var byStatus   = data.by_status  || {};
    var bySubAgent = data.by_sub_agent || {};

    var hasData = (total.cost_usd > 0) ||
      Object.keys(byStatus).length > 0 ||
      Object.keys(bySubAgent).length > 0;

    if (!hasData && data.task_count === 0) {
      setState('empty');
      return;
    }

    // Summary bar.
    var periodLabel = data.period_days === 0 ? 'all time' : 'last ' + data.period_days + ' days';
    summaryEl.textContent =
      data.task_count + ' task' + (data.task_count === 1 ? '' : 's') + ' \u00b7 ' + periodLabel +
      ' \u00b7 total cost: ' + (total.cost_usd ? '$' + total.cost_usd.toFixed(4) : '$0.0000');

    // By-Status table.
    byStatusTbody.innerHTML = '';
    var statusKeys = Object.keys(byStatus).sort();
    if (statusKeys.length === 0) {
      var emptyRow = document.createElement('tr');
      emptyRow.innerHTML = '<td colspan="5" style="padding:12px 10px;text-align:center;color:var(--text-muted);font-size:12px;">No data</td>';
      byStatusTbody.appendChild(emptyRow);
    } else {
      statusKeys.forEach(function (status) {
        byStatusTbody.appendChild(usageRow(statusBadge(status), byStatus[status], false));
      });
      byStatusTbody.appendChild(usageRow('<strong>Total</strong>', total, true));
    }

    // By-Sub-Agent table.
    bySubAgentTbody.innerHTML = '';
    var agentKeys = Object.keys(bySubAgent).sort();
    if (agentKeys.length === 0) {
      var emptyRow2 = document.createElement('tr');
      emptyRow2.innerHTML = '<td colspan="5" style="padding:12px 10px;text-align:center;color:var(--text-muted);font-size:12px;">No data</td>';
      bySubAgentTbody.appendChild(emptyRow2);
    } else {
      agentKeys.forEach(function (key) {
        bySubAgentTbody.appendChild(usageRow(agentLabel(key), bySubAgent[key], false));
      });
      bySubAgentTbody.appendChild(usageRow('<strong>Total</strong>', total, true));
    }

    setState('content');
  }

  window.showUsageStats = function () {
    modal.classList.remove('hidden');
    modal.style.display = 'flex';
    fetchStats();
  };

  window.closeUsageStats = function () {
    modal.classList.add('hidden');
    modal.style.display = '';
  };

  document.addEventListener('DOMContentLoaded', init);
})();
