// --- Log render schedulers ---
// renderLogs / renderTestLogs re-parse the entire buffer from scratch on every
// call, so we must not call them on every incoming network chunk.  Instead we
// batch pending updates and flush at most once per animation frame.
let _logRenderPending = false;
function scheduleLogRender() {
  if (_logRenderPending) return;
  _logRenderPending = true;
  requestAnimationFrame(function() {
    _logRenderPending = false;
    renderLogs();
  });
}

let _testLogRenderPending = false;
function scheduleTestLogRender() {
  if (_testLogRenderPending) return;
  _testLogRenderPending = true;
  requestAnimationFrame(function() {
    _testLogRenderPending = false;
    renderTestLogs();
  });
}

function _isCurrentModalSeq(seq) {
  return typeof seq !== 'number' || _modalState.seq === seq;
}

function _modalApiJson(url, signal) {
  if (typeof api === 'function') return api(url, { signal: signal });
  return fetch(url, { signal: signal }).then(function(res) { return res.json(); });
}

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
    // Defer scroll-to-bottom so the browser can batch the layout triggered by
    // the innerHTML/textContent write with the scroll update, avoiding a forced
    // synchronous reflow.
    requestAnimationFrame(function() {
      logsEl.scrollTop = logsEl.scrollHeight;
    });
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
  ['implementation', 'testing', 'changes', 'spans', 'timeline'].forEach(function(t) {
    const btn = document.getElementById('right-tab-' + t);
    const panel = document.getElementById('right-panel-' + t);
    const active = t === tab;
    if (btn) btn.classList.toggle('active', active);
    if (panel) panel.classList.toggle('hidden', !active);
  });
  if (tab === 'spans' && typeof loadFlamegraph !== 'undefined' && getOpenModalTaskId()) {
    loadFlamegraph(getOpenModalTaskId());
  }
  if (tab === 'timeline') {
    if (getOpenModalTaskId()) {
      renderTimeline(getOpenModalTaskId());
      _startTimelineRefresh(getOpenModalTaskId());
    }
  } else {
    _stopTimelineRefresh();
  }
  if (getOpenModalTaskId()) {
    history.replaceState(null, '', '#' + getOpenModalTaskId() + '/' + tab);
  }
}

function setLogsMode(mode) {
  logsMode = mode;
  renderLogs();
}

function startLogStream(id, seq) {
  logsMode = 'pretty';
  oversightData = null;
  // Pre-fetch oversight to decide the default view: switch to oversight only if
  // a ready summary already exists.
  oversightFetching = true;
  var signal = _modalState.abort ? _modalState.abort.signal : undefined;
  _modalApiJson('/api/tasks/' + id + '/oversight', signal)
    .then(function(data) {
      if (getOpenModalTaskId() !== id) return;
      if (!_isCurrentModalSeq(seq)) return;
      oversightData = data;
      oversightFetching = false;
      if (data.status === 'ready') {
        if (data.phase_count != null) {
          cardOversightCache.set(id, { phase_count: data.phase_count, phases: data.phases || [] });
          scheduleRender();
        }
        logsMode = 'oversight';
        renderLogs();
      }
    })
    .catch(function(err) {
      if (err && err.name === 'AbortError') return;
      oversightFetching = false;
    });
  _fetchLogs(id, null, seq);
}

// Fetch implementation-phase logs once (no reconnect — they are static by the
// time the test agent runs).
function startImplLogFetch(id, seq) {
  logsMode = 'pretty';
  oversightData = null;
  // Pre-fetch oversight to decide default view.
  oversightFetching = true;
  var signal = _modalState.abort ? _modalState.abort.signal : undefined;
  _modalApiJson('/api/tasks/' + id + '/oversight', signal)
    .then(function(data) {
      if (getOpenModalTaskId() !== id) return;
      if (!_isCurrentModalSeq(seq)) return;
      oversightData = data;
      oversightFetching = false;
      if (data.status === 'ready') {
        if (data.phase_count != null) {
          cardOversightCache.set(id, { phase_count: data.phase_count, phases: data.phases || [] });
          scheduleRender();
        }
        logsMode = 'oversight';
        renderLogs();
      }
    })
    .catch(function(err) {
      if (err && err.name === 'AbortError') return;
      oversightFetching = false;
    });
  rawLogBuffer = '';
  document.getElementById('modal-logs').innerHTML = '';
  const decoder = new TextDecoder();
  fetch(`/api/tasks/${id}/logs?phase=impl`, { signal: signal })
    .then(res => {
      if (!res.ok || !res.body) return;
      const reader = res.body.getReader();
      function read() {
        reader.read().then(({ done, value }) => {
          if (getOpenModalTaskId() !== id) return;
          if (!_isCurrentModalSeq(seq)) return;
          if (done) { renderLogs(); return; }
          rawLogBuffer += decoder.decode(value, { stream: true });
          scheduleLogRender();
          read();
        }).catch(() => {});
      }
      read();
    })
    .catch((err) => {
      if (err && err.name === 'AbortError') return;
    });
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
    requestAnimationFrame(function() {
      logsEl.scrollTop = logsEl.scrollHeight;
    });
  }
}

function setTestLogsMode(mode) {
  testLogsMode = mode;
  renderTestLogs();
}

function startTestLogStream(id, seq) {
  testLogsMode = 'pretty';
  testOversightData = null;
  // Pre-fetch test oversight to decide default view.
  testOversightFetching = true;
  var signal = _modalState.abort ? _modalState.abort.signal : undefined;
  _modalApiJson('/api/tasks/' + id + '/oversight/test', signal)
    .then(function(data) {
      if (getOpenModalTaskId() !== id) return;
      if (!_isCurrentModalSeq(seq)) return;
      testOversightData = data;
      testOversightFetching = false;
      if (data.status === 'ready') {
        testLogsMode = 'oversight';
        renderTestLogs();
      }
    })
    .catch(function(err) {
      if (err && err.name === 'AbortError') return;
      testOversightFetching = false;
    });
  _fetchTestLogs(id, null, seq);
}

function _fetchTestLogs(id, retryDelay, seq) {
  if (getOpenModalTaskId() !== id) return;
  if (!_isCurrentModalSeq(seq)) return;
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
    if (getOpenModalTaskId() !== id) return;
    if (!_isCurrentModalSeq(seq)) return;
    const task = tasks.find(t => t.id === id);
    if (!task || (task.status !== 'in_progress' && task.status !== 'committing')) return;
    const nextDelay = Math.min(delay * 2, 15000);
    setTimeout(() => _fetchTestLogs(id, nextDelay, seq), delay);
  }

  fetch(url, { signal: testLogsAbort.signal })
    .then(res => {
      if (!res.ok || !res.body) { reconnect(); return; }
      const reader = res.body.getReader();
      function read() {
        reader.read().then(({ done, value }) => {
          if (getOpenModalTaskId() !== id) return;
          if (!_isCurrentModalSeq(seq)) return;
          if (done) { reconnect(); return; }
          testRawLogBuffer += decoder.decode(value, { stream: true });
          scheduleTestLogRender();
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

function _fetchLogs(id, retryDelay, seq) {
  // Guard: if the modal was closed or switched to a different task since this
  // call was scheduled (e.g. by a reconnect setTimeout), bail out so we don't
  // hijack the log stream or mix logs from a stale task into the buffer.
  if (getOpenModalTaskId() !== id) return;
  if (!_isCurrentModalSeq(seq)) return;
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
    if (getOpenModalTaskId() !== id) return;
    if (!_isCurrentModalSeq(seq)) return;
    const task = tasks.find(t => t.id === id);
    if (!task || (task.status !== 'in_progress' && task.status !== 'committing')) return;
    const nextDelay = Math.min(delay * 2, 15000);
    setTimeout(() => _fetchLogs(id, nextDelay, seq), delay);
  }

  fetch(url, { signal: logsAbort.signal })
    .then(res => {
      if (!res.ok || !res.body) { reconnect(); return; }
      const reader = res.body.getReader();
      function read() {
        reader.read().then(({ done, value }) => {
          if (getOpenModalTaskId() !== id) return;
          if (!_isCurrentModalSeq(seq)) return;
          if (done) { reconnect(); return; }
          rawLogBuffer += decoder.decode(value, { stream: true });
          scheduleLogRender();
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
