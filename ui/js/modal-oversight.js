// --- Oversight view ---

// oversightData caches the last fetched oversight for the open task.
// Cleared when the modal opens a different task.
let oversightData = null;
let oversightFetching = false;

// testOversightData caches the last fetched test oversight for the open task.
let testOversightData = null;
let testOversightFetching = false;

function _fetchOversightJson(url, signal) {
  if (typeof api === 'function') return api(url, { signal: signal });
  return fetch(url, { signal: signal }).then(function(res) { return res.json(); });
}

function renderOversightPhases(phases) {
  return buildPhaseListHTML(phases);
}

function renderOversightInLogs() {
  const logsEl = document.getElementById('modal-logs');
  const seq = typeof modalLoadSeq === 'number' ? modalLoadSeq : null;
  if (!oversightData) {
    if (!oversightFetching && currentTaskId) {
      oversightFetching = true;
      const id = currentTaskId;
      const signal = (typeof modalAbort !== 'undefined' && modalAbort) ? modalAbort.signal : undefined;
      _fetchOversightJson('/api/tasks/' + id + '/oversight', signal)
        .then(function(data) {
          if (currentTaskId !== id) return;
          if (seq !== null && typeof modalLoadSeq === 'number' && modalLoadSeq !== seq) return;
          oversightData = data;
          oversightFetching = false;
          if (logsMode === 'oversight') renderLogs();
        })
        .catch(function(err) {
          if (err && err.name === 'AbortError') return;
          oversightFetching = false;
          if (currentTaskId === id && (seq === null || (typeof modalLoadSeq === 'number' && modalLoadSeq === seq)) && logsMode === 'oversight') {
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
        if (logsMode === 'oversight' && currentTaskId && (seq === null || (typeof modalLoadSeq === 'number' && modalLoadSeq === seq))) {
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
  const seq = typeof modalLoadSeq === 'number' ? modalLoadSeq : null;
  if (!testOversightData) {
    if (!testOversightFetching && currentTaskId) {
      testOversightFetching = true;
      const id = currentTaskId;
      const signal = (typeof modalAbort !== 'undefined' && modalAbort) ? modalAbort.signal : undefined;
      _fetchOversightJson('/api/tasks/' + id + '/oversight/test', signal)
        .then(function(data) {
          if (currentTaskId !== id) return;
          if (seq !== null && typeof modalLoadSeq === 'number' && modalLoadSeq !== seq) return;
          testOversightData = data;
          testOversightFetching = false;
          if (testLogsMode === 'oversight') renderTestLogs();
        })
        .catch(function(err) {
          if (err && err.name === 'AbortError') return;
          testOversightFetching = false;
          if (currentTaskId === id && (seq === null || (typeof modalLoadSeq === 'number' && modalLoadSeq === seq)) && testLogsMode === 'oversight') {
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
        if (testLogsMode === 'oversight' && currentTaskId && (seq === null || (typeof modalLoadSeq === 'number' && modalLoadSeq === seq))) {
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
