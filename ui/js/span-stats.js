// --- Span Statistics Modal ---

(function () {
  var modal, loadingEl, errorEl, emptyEl, contentEl, summaryEl, tbody;

  function init() {
    modal     = document.getElementById('span-stats-modal');
    loadingEl = document.getElementById('span-stats-loading');
    errorEl   = document.getElementById('span-stats-error');
    emptyEl   = document.getElementById('span-stats-empty');
    contentEl = document.getElementById('span-stats-content');
    summaryEl = document.getElementById('span-stats-summary');
    tbody     = document.getElementById('span-stats-tbody');

    modal.addEventListener('click', function (e) {
      if (e.target === modal) closeSpanStats();
    });
  }

  function setState(state, msg) {
    loadingEl.style.display = state === 'loading' ? 'flex' : 'none';
    errorEl.classList.toggle('hidden',   state !== 'error');
    emptyEl.classList.toggle('hidden',   state !== 'empty');
    contentEl.classList.toggle('hidden', state !== 'table');
    if (state === 'error') errorEl.textContent = msg || 'Unknown error';
  }

  function fetchStats() {
    setState('loading');
    fetch('/api/debug/spans')
      .then(function (res) {
        return res.json().then(function (data) { return { ok: res.ok, data: data }; });
      })
      .then(function (result) {
        if (!result.ok) { setState('error', result.data.error || JSON.stringify(result.data)); return; }
        renderStats(result.data);
      })
      .catch(function (err) { setState('error', String(err)); });
  }

  function renderStats(data) {
    var phases = data.phases || {};
    var keys = Object.keys(phases).sort();

    if (keys.length === 0) { setState('empty'); return; }

    summaryEl.textContent =
      data.tasks_scanned + ' tasks scanned \u00b7 ' + data.spans_total + ' spans total';

    tbody.innerHTML = '';
    keys.forEach(function (phase) {
      var s = phases[phase];
      var tr = document.createElement('tr');
      tr.style.cssText = 'border-bottom: 1px solid var(--border); transition: background 0.1s;';
      tr.addEventListener('mouseenter', function () { tr.style.background = 'var(--bg-raised)'; });
      tr.addEventListener('mouseleave', function () { tr.style.background = ''; });
      tr.innerHTML =
        '<td style="padding:6px 10px;font-weight:500;">' + escapeHtml(phase) + '</td>' +
        '<td style="padding:6px 10px;text-align:right;color:var(--text-muted);">' + s.count + '</td>' +
        '<td style="padding:6px 10px;text-align:right;">' + fmtMs(s.min_ms) + '</td>' +
        '<td style="padding:6px 10px;text-align:right;">' + fmtMs(s.p50_ms) + '</td>' +
        '<td style="padding:6px 10px;text-align:right;font-weight:600;">' + fmtMs(s.p95_ms) + '</td>' +
        '<td style="padding:6px 10px;text-align:right;">' + fmtMs(s.p99_ms) + '</td>' +
        '<td style="padding:6px 10px;text-align:right;color:var(--text-muted);">' + fmtMs(s.max_ms) + '</td>';
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
    modal.classList.remove('hidden');
    modal.style.display = 'flex';
    fetchStats();
  };

  window.closeSpanStats = function () {
    modal.classList.add('hidden');
    modal.style.display = '';
  };

  document.addEventListener('DOMContentLoaded', init);
})();
