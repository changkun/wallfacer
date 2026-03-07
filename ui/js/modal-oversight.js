// --- Oversight view ---

// oversightData caches the last fetched oversight for the open task.
// Cleared when the modal opens a different task.
let oversightData = null;
let oversightFetching = false;

// testOversightData caches the last fetched test oversight for the open task.
let testOversightData = null;
let testOversightFetching = false;

function renderOversightPhases(phases) {
  if (!phases || phases.length === 0) {
    return '<div class="oversight-empty">No phases recorded.</div>';
  }
  return phases.map(function(phase, i) {
    const ts = phase.timestamp ? new Date(phase.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '';
    const tools = (phase.tools_used || []).map(function(t) {
      return '<span class="oversight-tool">' + escapeHtml(t) + '</span>';
    }).join('');
    const commands = (phase.commands || []).map(function(c) {
      return '<li class="oversight-command">' + escapeHtml(c) + '</li>';
    }).join('');
    const actions = (phase.actions || []).map(function(a) {
      return '<li class="oversight-action">' + escapeHtml(a) + '</li>';
    }).join('');
    return '<div class="oversight-phase">' +
      '<div class="oversight-phase-header">' +
        '<span class="oversight-phase-num">Phase ' + (i + 1) + '</span>' +
        '<span class="oversight-phase-title">' + escapeHtml(phase.title || '') + '</span>' +
        (ts ? '<span class="oversight-phase-time">' + ts + '</span>' : '') +
      '</div>' +
      (phase.summary ? '<div class="oversight-summary">' + escapeHtml(phase.summary) + '</div>' : '') +
      (tools ? '<div class="oversight-tools">' + tools + '</div>' : '') +
      (commands ? '<ul class="oversight-commands">' + commands + '</ul>' : '') +
      (actions ? '<ul class="oversight-actions">' + actions + '</ul>' : '') +
    '</div>';
  }).join('');
}

function renderOversightInLogs() {
  const logsEl = document.getElementById('modal-logs');
  if (!oversightData) {
    if (!oversightFetching && currentTaskId) {
      oversightFetching = true;
      const id = currentTaskId;
      fetch('/api/tasks/' + id + '/oversight')
        .then(function(res) { return res.json(); })
        .then(function(data) {
          if (currentTaskId !== id) return;
          oversightData = data;
          oversightFetching = false;
          if (logsMode === 'oversight') renderLogs();
        })
        .catch(function() {
          oversightFetching = false;
          if (currentTaskId === id && logsMode === 'oversight') {
            logsEl.innerHTML = '<div class="oversight-error">Failed to load oversight summary.</div>';
          }
        });
    }
    logsEl.innerHTML = '<div class="oversight-loading">Fetching oversight summary\u2026</div>';
    return;
  }

  switch (oversightData.status) {
    case 'pending':
      logsEl.innerHTML = '<div class="oversight-loading">Oversight summary not yet generated.</div>';
      break;
    case 'generating':
      logsEl.innerHTML = '<div class="oversight-loading">Generating oversight summary\u2026</div>';
      // Poll again after a delay.
      setTimeout(function() {
        if (logsMode === 'oversight' && currentTaskId) {
          oversightData = null;
          renderLogs();
        }
      }, 3000);
      break;
    case 'failed':
      logsEl.innerHTML = '<div class="oversight-error">Oversight generation failed' +
        (oversightData.error ? ': ' + escapeHtml(oversightData.error) : '') + '</div>';
      break;
    case 'ready':
      logsEl.innerHTML = '<div class="oversight-view">' + renderOversightPhases(oversightData.phases) + '</div>';
      break;
    default:
      logsEl.innerHTML = '<div class="oversight-loading">Loading\u2026</div>';
  }
}

function renderTestOversightInTestLogs() {
  const logsEl = document.getElementById('modal-test-logs');
  if (!testOversightData) {
    if (!testOversightFetching && currentTaskId) {
      testOversightFetching = true;
      const id = currentTaskId;
      fetch('/api/tasks/' + id + '/oversight/test')
        .then(function(res) { return res.json(); })
        .then(function(data) {
          if (currentTaskId !== id) return;
          testOversightData = data;
          testOversightFetching = false;
          if (testLogsMode === 'oversight') renderTestLogs();
        })
        .catch(function() {
          testOversightFetching = false;
          if (currentTaskId === id && testLogsMode === 'oversight') {
            logsEl.innerHTML = '<div class="oversight-error">Failed to load test oversight summary.</div>';
          }
        });
    }
    logsEl.innerHTML = '<div class="oversight-loading">Fetching test oversight summary\u2026</div>';
    return;
  }

  switch (testOversightData.status) {
    case 'pending':
      logsEl.innerHTML = '<div class="oversight-loading">Test oversight summary not yet generated.</div>';
      break;
    case 'generating':
      logsEl.innerHTML = '<div class="oversight-loading">Generating test oversight summary\u2026</div>';
      setTimeout(function() {
        if (testLogsMode === 'oversight' && currentTaskId) {
          testOversightData = null;
          renderTestLogs();
        }
      }, 3000);
      break;
    case 'failed':
      logsEl.innerHTML = '<div class="oversight-error">Test oversight generation failed' +
        (testOversightData.error ? ': ' + escapeHtml(testOversightData.error) : '') + '</div>';
      break;
    case 'ready':
      logsEl.innerHTML = '<div class="oversight-view">' + renderOversightPhases(testOversightData.phases) + '</div>';
      break;
    default:
      logsEl.innerHTML = '<div class="oversight-loading">Loading\u2026</div>';
  }
}
