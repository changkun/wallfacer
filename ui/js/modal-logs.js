// --- Log rendering and streaming ---

function _updateLogsTabs() {
  ['oversight', 'pretty', 'raw'].forEach(function(m) {
    const tab = document.getElementById('logs-tab-' + m);
    if (tab) tab.classList.toggle('active', m === logsMode);
  });
  const searchBar = document.getElementById('log-search-bar');
  if (searchBar) {
    searchBar.style.display = (logsMode === 'oversight') ? 'none' : 'flex';
  }
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
  const countEl = document.getElementById('log-search-count');
  if (logsMode === 'pretty') {
    if (logSearchQuery) {
      const query = logSearchQuery.toLowerCase();
      const allLines = rawLogBuffer.split('\n').filter(function(l) { return l.trim().length > 0; });
      const filteredLines = allLines.filter(function(line) {
        return line.replace(/\x1b\[[0-9;]*[a-zA-Z]/g, '').toLowerCase().includes(query);
      });
      logsEl.innerHTML = renderPrettyLogs(filteredLines.join('\n'));
      highlightLogMatches(logSearchQuery);
      if (countEl) countEl.textContent = filteredLines.length + ' / ' + allLines.length + ' lines';
    } else {
      logsEl.innerHTML = renderPrettyLogs(rawLogBuffer);
      if (countEl) countEl.textContent = '';
    }
  } else {
    const stripped = rawLogBuffer.replace(/\x1b\[[0-9;]*[a-zA-Z]/g, '');
    if (logSearchQuery) {
      const query = logSearchQuery.toLowerCase();
      const allLines = stripped.split('\n').filter(function(l) { return l.trim().length > 0; });
      const filteredLines = allLines.filter(function(line) {
        return line.toLowerCase().includes(query);
      });
      logsEl.textContent = filteredLines.join('\n');
      if (countEl) countEl.textContent = filteredLines.length + ' / ' + allLines.length + ' lines';
    } else {
      logsEl.textContent = stripped;
      if (countEl) countEl.textContent = '';
    }
  }
  if (!logSearchQuery && atBottom) {
    logsEl.scrollTop = logsEl.scrollHeight;
  }
}

function highlightLogMatches(query) {
  if (!query) return;
  const logsEl = document.getElementById('modal-logs');
  const escaped = query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
  const re = new RegExp(escaped, 'gi');
  const walker = document.createTreeWalker(logsEl, NodeFilter.SHOW_TEXT);
  const textNodes = [];
  let node;
  while ((node = walker.nextNode())) textNodes.push(node);
  textNodes.forEach(function(tn) {
    if (!re.test(tn.textContent)) return;
    re.lastIndex = 0;
    const wrapper = document.createElement('span');
    wrapper.innerHTML = tn.textContent.replace(
      re,
      '<mark style="background:#fef08a;color:#1a1917;border-radius:2px;">$&</mark>'
    );
    tn.parentNode.replaceChild(wrapper, tn);
  });
}

function onLogSearchInput(value) {
  logSearchQuery = value.trim();
  renderLogs();
}

function setRightTab(tab) {
  ['implementation', 'testing', 'changes', 'spans'].forEach(function(t) {
    const btn = document.getElementById('right-tab-' + t);
    const panel = document.getElementById('right-panel-' + t);
    const active = t === tab;
    if (btn) btn.classList.toggle('active', active);
    if (panel) panel.classList.toggle('hidden', !active);
  });
  if (tab === 'spans' && typeof loadFlamegraph !== 'undefined' && typeof currentTaskId !== 'undefined' && currentTaskId) {
    loadFlamegraph(currentTaskId);
  }
}

function setLogsMode(mode) {
  logsMode = mode;
  renderLogs();
}

function startLogStream(id) {
  logsMode = 'pretty';
  oversightData = null;
  // Pre-fetch oversight to decide the default view: switch to oversight only if
  // a ready summary already exists.
  oversightFetching = true;
  fetch('/api/tasks/' + id + '/oversight')
    .then(function(res) { return res.json(); })
    .then(function(data) {
      if (currentTaskId !== id) return;
      oversightData = data;
      oversightFetching = false;
      if (data.status === 'ready') {
        logsMode = 'oversight';
        renderLogs();
      }
    })
    .catch(function() { oversightFetching = false; });
  _fetchLogs(id);
}

// Fetch implementation-phase logs once (no reconnect — they are static by the
// time the test agent runs).
function startImplLogFetch(id) {
  logsMode = 'pretty';
  oversightData = null;
  // Pre-fetch oversight to decide default view.
  oversightFetching = true;
  fetch('/api/tasks/' + id + '/oversight')
    .then(function(res) { return res.json(); })
    .then(function(data) {
      if (currentTaskId !== id) return;
      oversightData = data;
      oversightFetching = false;
      if (data.status === 'ready') {
        logsMode = 'oversight';
        renderLogs();
      }
    })
    .catch(function() { oversightFetching = false; });
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
  testLogsMode = 'pretty';
  testOversightData = null;
  // Pre-fetch test oversight to decide default view.
  testOversightFetching = true;
  fetch('/api/tasks/' + id + '/oversight/test')
    .then(function(res) { return res.json(); })
    .then(function(data) {
      if (currentTaskId !== id) return;
      testOversightData = data;
      testOversightFetching = false;
      if (data.status === 'ready') {
        testLogsMode = 'oversight';
        renderTestLogs();
      }
    })
    .catch(function() { testOversightFetching = false; });
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
