// --- Oversight view ---

// oversightData caches the last fetched oversight for the open task.
// Cleared when the modal opens a different task.
let oversightData = null;
let oversightFetching = false;

// testOversightData caches the last fetched test oversight for the open task.
let testOversightData = null;
let testOversightFetching = false;

function _fetchOversightJson(url, signal) {
  if (typeof apiGet === "function") return apiGet(url, { signal: signal });
  return fetch(url, { signal: signal }).then(function (res) {
    return res.json();
  });
}

function renderOversightPhases(phases) {
  return buildPhaseListHTML(phases);
}

function renderOversightInLogs() {
  const logsEl = document.getElementById("modal-logs");
  const seq = _modalState.seq;
  if (!oversightData) {
    if (!oversightFetching && getOpenModalTaskId()) {
      oversightFetching = true;
      const id = getOpenModalTaskId();
      const signal = _modalState.abort ? _modalState.abort.signal : undefined;
      _fetchOversightJson("/api/tasks/" + id + "/oversight", signal)
        .then(function (data) {
          if (getOpenModalTaskId() !== id) return;
          if (_modalState.seq !== seq) return;
          oversightData = data;
          oversightFetching = false;
          if (data && data.status === "ready" && data.phase_count != null) {
            cardOversightCache.set(id, {
              phase_count: data.phase_count,
              phases: data.phases || [],
            });
            scheduleRender();
          }
          if (logsMode === "oversight") renderLogs();
        })
        .catch(function (err) {
          if (err && err.name === "AbortError") return;
          oversightFetching = false;
          if (
            getOpenModalTaskId() === id &&
            _modalState.seq === seq &&
            logsMode === "oversight"
          ) {
            logsEl.innerHTML =
              '<div class="oversight-error">Failed to load oversight summary.</div>';
          }
        });
    }
    logsEl.innerHTML =
      '<div class="oversight-loading">Fetching oversight summary\u2026</div>';
    return;
  }

  switch (oversightData.status) {
    case "pending":
      logsEl.innerHTML =
        '<div class="oversight-loading">Oversight summary not yet generated.</div>';
      break;
    case "generating":
      logsEl.innerHTML =
        '<div class="oversight-loading">Generating oversight summary\u2026</div>';
      // Poll again after a delay.
      setTimeout(function () {
        if (
          logsMode === "oversight" &&
          getOpenModalTaskId() &&
          _modalState.seq === seq
        ) {
          oversightData = null;
          renderLogs();
        }
      }, 3000);
      break;
    case "failed":
      logsEl.innerHTML =
        '<div class="oversight-error">Oversight generation failed' +
        (oversightData.error ? ": " + escapeHtml(oversightData.error) : "") +
        "</div>";
      break;
    case "ready":
      logsEl.innerHTML =
        '<div class="oversight-view">' +
        renderOversightPhases(oversightData.phases) +
        "</div>";
      break;
    default:
      logsEl.innerHTML = '<div class="oversight-loading">Loading\u2026</div>';
  }
}

function renderTestOversightInTestLogs() {
  const logsEl = document.getElementById("modal-test-logs");
  const seq = _modalState.seq;
  if (!testOversightData) {
    if (!testOversightFetching && getOpenModalTaskId()) {
      testOversightFetching = true;
      const id = getOpenModalTaskId();
      const signal = _modalState.abort ? _modalState.abort.signal : undefined;
      _fetchOversightJson("/api/tasks/" + id + "/oversight/test", signal)
        .then(function (data) {
          if (getOpenModalTaskId() !== id) return;
          if (_modalState.seq !== seq) return;
          testOversightData = data;
          testOversightFetching = false;
          if (testLogsMode === "oversight") renderTestLogs();
        })
        .catch(function (err) {
          if (err && err.name === "AbortError") return;
          testOversightFetching = false;
          if (
            getOpenModalTaskId() === id &&
            _modalState.seq === seq &&
            testLogsMode === "oversight"
          ) {
            logsEl.innerHTML =
              '<div class="oversight-error">Failed to load test oversight summary.</div>';
          }
        });
    }
    logsEl.innerHTML =
      '<div class="oversight-loading">Fetching test oversight summary\u2026</div>';
    return;
  }

  switch (testOversightData.status) {
    case "pending":
      logsEl.innerHTML =
        '<div class="oversight-loading">Test oversight summary not yet generated.</div>';
      break;
    case "generating":
      logsEl.innerHTML =
        '<div class="oversight-loading">Generating test oversight summary\u2026</div>';
      setTimeout(function () {
        if (
          testLogsMode === "oversight" &&
          getOpenModalTaskId() &&
          _modalState.seq === seq
        ) {
          testOversightData = null;
          renderTestLogs();
        }
      }, 3000);
      break;
    case "failed":
      logsEl.innerHTML =
        '<div class="oversight-error">Test oversight generation failed' +
        (testOversightData.error
          ? ": " + escapeHtml(testOversightData.error)
          : "") +
        "</div>";
      break;
    case "ready":
      logsEl.innerHTML =
        '<div class="oversight-view">' +
        renderOversightPhases(testOversightData.phases) +
        "</div>";
      break;
    default:
      logsEl.innerHTML = '<div class="oversight-loading">Loading\u2026</div>';
  }
}
