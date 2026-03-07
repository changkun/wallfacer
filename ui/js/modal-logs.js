// --- Log rendering and streaming ---

function _updateLogsTabs() {
  ['oversight', 'pretty', 'raw'].forEach(function(m) {
    const tab = document.getElementById('logs-tab-' + m);
    if (tab) tab.classList.toggle('active', m === logsMode);
  });
}

function renderLogs() {
  const logsEl = document.getElementById('modal-logs');
  _updateLogsTabs();
  logsEl.classList.toggle('oversight-mode', logsMode === 'oversight');
  if (logsMode === 'oversight') {
    renderOversightInLogs();
    return;
  }
  // Capture scroll position before updating content so we know if the user was at the bottom.
  const atBottom = logsEl.scrollHeight - logsEl.scrollTop - logsEl.clientHeight < 80;
  if (logsMode === 'pretty') {
    logsEl.innerHTML = renderPrettyLogs(rawLogBuffer);
  } else {
    logsEl.textContent = rawLogBuffer.replace(/\x1b\[[0-9;]*[a-zA-Z]/g, '');
  }
  if (atBottom) {
    logsEl.scrollTop = logsEl.scrollHeight;
  }
}

function setRightTab(tab) {
  ['implementation', 'testing', 'changes'].forEach(function(t) {
    const btn = document.getElementById('right-tab-' + t);
    const panel = document.getElementById('right-panel-' + t);
    const active = t === tab;
    if (btn) btn.classList.toggle('active', active);
    if (panel) panel.classList.toggle('hidden', !active);
  });
}

function setLogsMode(mode) {
  logsMode = mode;
  renderLogs();
}

function startLogStream(id) {
  const task = tasks.find(t => t.id === id);
  logsMode = (task && task.status === 'done') ? 'oversight' : 'pretty';
  oversightData = null;
  oversightFetching = false;
  _fetchLogs(id);
}

// Fetch implementation-phase logs once (no reconnect — they are static by the
// time the test agent runs).
function startImplLogFetch(id) {
  logsMode = 'oversight';
  oversightData = null;
  oversightFetching = false;
  rawLogBuffer = '';
  document.getElementById('modal-logs').innerHTML = '';
  const decoder = new TextDecoder();
  fetch(`/api/tasks/${id}/logs?phase=impl`)
    .then(res => {
      if (!res.ok || !res.body) return;
      const reader = res.body.getReader();
      function read() {
        reader.read().then(({ done, value }) => {
          if (done) { renderLogs(); return; }
          rawLogBuffer += decoder.decode(value, { stream: true });
          renderLogs();
          read();
        }).catch(() => {});
      }
      read();
    })
    .catch(() => {});
}

function _updateTestLogsTabs() {
  ['oversight', 'pretty', 'raw'].forEach(function(m) {
    const tab = document.getElementById('test-logs-tab-' + m);
    if (tab) tab.classList.toggle('active', m === testLogsMode);
  });
}

function renderTestLogs() {
  const logsEl = document.getElementById('modal-test-logs');
  _updateTestLogsTabs();
  logsEl.classList.toggle('oversight-mode', testLogsMode === 'oversight');
  if (testLogsMode === 'oversight') {
    renderTestOversightInTestLogs();
    return;
  }
  const atBottom = logsEl.scrollHeight - logsEl.scrollTop - logsEl.clientHeight < 80;
  if (testLogsMode === 'pretty') {
    logsEl.innerHTML = renderPrettyLogs(testRawLogBuffer);
  } else {
    logsEl.textContent = testRawLogBuffer.replace(/\x1b\[[0-9;]*[a-zA-Z]/g, '');
  }
  if (atBottom) {
    logsEl.scrollTop = logsEl.scrollHeight;
  }
}

function setTestLogsMode(mode) {
  testLogsMode = mode;
  renderTestLogs();
}

function startTestLogStream(id) {
  const task = tasks.find(t => t.id === id);
  testLogsMode = (task && (task.status === 'done' || task.status === 'waiting')) ? 'oversight' : 'pretty';
  testOversightData = null;
  testOversightFetching = false;
  _fetchTestLogs(id);
}

function _fetchTestLogs(id, retryDelay) {
  if (currentTaskId !== id) return;
  if (testLogsAbort) testLogsAbort.abort();
  testLogsAbort = new AbortController();
  if (!retryDelay) {
    testRawLogBuffer = '';
    document.getElementById('modal-test-logs').innerHTML = '';
  }
  const delay = retryDelay || 1000;
  const decoder = new TextDecoder();
  // For completed tasks use phase=test to serve only test-agent turns (those
  // after TestRunStartTurn). For running tasks keep streaming all live logs.
  const task = tasks.find(t => t.id === id);
  const isRunning = task && (task.status === 'in_progress' || task.status === 'committing');
  const url = isRunning ? `/api/tasks/${id}/logs?raw=true` : `/api/tasks/${id}/logs?phase=test`;

  function reconnect() {
    if (currentTaskId !== id) return;
    const task = tasks.find(t => t.id === id);
    if (!task || (task.status !== 'in_progress' && task.status !== 'committing')) return;
    const nextDelay = Math.min(delay * 2, 15000);
    setTimeout(() => _fetchTestLogs(id, nextDelay), delay);
  }

  fetch(url, { signal: testLogsAbort.signal })
    .then(res => {
      if (!res.ok || !res.body) { reconnect(); return; }
      const reader = res.body.getReader();
      function read() {
        reader.read().then(({ done, value }) => {
          if (done) { reconnect(); return; }
          testRawLogBuffer += decoder.decode(value, { stream: true });
          renderTestLogs();
          read();
        }).catch(() => reconnect());
      }
      read();
    })
    .catch(err => {
      if (err.name === 'AbortError') return;
      reconnect();
    });
}

function _fetchLogs(id, retryDelay) {
  // Guard: if the modal was closed or switched to a different task since this
  // call was scheduled (e.g. by a reconnect setTimeout), bail out so we don't
  // hijack the log stream or mix logs from a stale task into the buffer.
  if (currentTaskId !== id) return;
  if (logsAbort) logsAbort.abort();
  logsAbort = new AbortController();
  if (!retryDelay) {
    rawLogBuffer = '';
    document.getElementById('modal-logs').innerHTML = '';
  }
  const delay = retryDelay || 1000;
  const decoder = new TextDecoder();
  const url = `/api/tasks/${id}/logs?raw=true`;

  function reconnect() {
    // Only reconnect if this task modal is still open and task is running.
    if (currentTaskId !== id) return;
    const task = tasks.find(t => t.id === id);
    if (!task || (task.status !== 'in_progress' && task.status !== 'committing')) return;
    const nextDelay = Math.min(delay * 2, 15000);
    setTimeout(() => _fetchLogs(id, nextDelay), delay);
  }

  fetch(url, { signal: logsAbort.signal })
    .then(res => {
      if (!res.ok || !res.body) { reconnect(); return; }
      const reader = res.body.getReader();
      function read() {
        reader.read().then(({ done, value }) => {
          if (done) { reconnect(); return; }
          rawLogBuffer += decoder.decode(value, { stream: true });
          renderLogs();
          read();
        }).catch(() => reconnect());
      }
      read();
    })
    .catch(err => {
      if (err.name === 'AbortError') return;
      reconnect();
    });
}
