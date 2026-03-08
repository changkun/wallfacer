// --- Multi-turn result rendering ---

function detectResultType(text) {
  if (!text) return 'result';
  const planPatterns = [
    /^#{1,3}\s+.*\bplan\b/im,
    /^#{1,3}\s+.*\bphase\s*\d/im,
    /^#{1,3}\s+.*\bstep\s*\d/im,
    /\bimplementation plan\b/i,
    /^#{1,3}\s+.*\bapproach\b/im,
    /^#{1,3}\s+.*\bproposal\b/im,
    /^#{1,3}\s+.*\bdesign\b/im,
    /^#{1,3}\s+.*\barchitecture\b/im,
    /^#{1,3}\s+.*\bstrategy\b/im,
  ];
  return planPatterns.some(function(p) { return p.test(text); }) ? 'plan' : 'result';
}

function copyResultEntry(entryId) {
  const rawEl = document.getElementById(entryId + '-raw');
  if (!rawEl) return;
  const text = rawEl.textContent;
  const btn = event.currentTarget;
  navigator.clipboard.writeText(text).then(function() {
    const origHTML = btn.innerHTML;
    btn.textContent = 'Copied!';
    setTimeout(function() { btn.innerHTML = origHTML; }, 1500);
  }).catch(function() {});
}

function toggleResultEntryRaw(entryId) {
  const renderedEl = document.getElementById(entryId + '-rendered');
  const rawEl = document.getElementById(entryId + '-raw');
  const btn = event.currentTarget;
  const showingRaw = !rawEl.classList.contains('hidden');
  if (showingRaw) {
    renderedEl.classList.remove('hidden');
    rawEl.classList.add('hidden');
    btn.textContent = 'Raw';
  } else {
    renderedEl.classList.add('hidden');
    rawEl.classList.remove('hidden');
    btn.textContent = 'Preview';
  }
}

const _copyIcon = '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>';

function setLeftTab(tab) {
  ['implementation', 'testing'].forEach(function(t) {
    const btn = document.getElementById('left-tab-' + t);
    const panel = document.getElementById('left-panel-' + t);
    const active = t === tab;
    if (btn) btn.classList.toggle('active', active);
    if (panel) panel.classList.toggle('hidden', !active);
  });
}

function renderResultsFromEvents(results, opts) {
  opts = opts || {};
  const tabId = opts.tabId || 'left-tab-implementation';
  const listId = opts.listId || 'modal-results-list';
  const entryPrefix = opts.entryPrefix || 'result-entry-';

  const summarySection = document.getElementById('modal-summary-section');
  const tabBtn = document.getElementById(tabId);
  const listEl = document.getElementById(listId);
  if (!results || results.length === 0) {
    if (tabBtn) tabBtn.classList.add('hidden');
    const anyVisible = ['implementation', 'testing'].some(function(t) {
      const btn = document.getElementById('left-tab-' + t);
      return btn && !btn.classList.contains('hidden');
    });
    if (!anyVisible && summarySection) summarySection.classList.add('hidden');
    return;
  }

  if (tabBtn) tabBtn.classList.remove('hidden');
  if (summarySection) summarySection.classList.remove('hidden');
  if (opts.autoSwitch) setLeftTab(tabId.replace('left-tab-', ''));

  const totalTurns = results.length;
  // Display newest turn first so the most recent result is immediately visible.
  listEl.innerHTML = [...results].reverse().map(function(result, i) {
    const isNewest = i === 0;
    const originalIndex = totalTurns - 1 - i; // chronological index (0-based)
    const isPlan = detectResultType(result) === 'plan';
    const entryId = entryPrefix + originalIndex;

    const typeBadgeHtml = isPlan
      ? '<span class="result-type-badge result-type-plan">Plan</span>'
      : '';
    const turnLabelHtml = totalTurns > 1
      ? '<span class="result-turn-label">Turn ' + (originalIndex + 1) + '</span>'
      : '';
    const labelsHtml = '<div class="result-entry-labels">' + typeBadgeHtml + turnLabelHtml + '</div>';
    const btnRowHtml =
      '<div class="flex items-center gap-1.5">' +
        '<button onclick="copyResultEntry(\'' + entryId + '\')" class="btn-icon" title="Copy">' +
          _copyIcon + ' Copy' +
        '</button>' +
        '<button onclick="toggleResultEntryRaw(\'' + entryId + '\')" class="btn-icon">Raw</button>' +
      '</div>';
    const bodyHtml =
      '<div id="' + entryId + '-rendered" class="result-entry-body prose-content">' + renderMarkdown(result) + '</div>' +
      '<pre id="' + entryId + '-raw" class="result-entry-body hidden">' + escapeHtml(result) + '</pre>';

    if (!isNewest) {
      return '<details class="result-entry">' +
        '<summary class="result-entry-summary">' + labelsHtml + '</summary>' +
        '<div class="result-entry-actions">' + btnRowHtml + '</div>' +
        bodyHtml +
        '</details>';
    } else {
      return '<div class="result-entry">' +
        '<div class="result-entry-header">' + labelsHtml + btnRowHtml + '</div>' +
        bodyHtml +
        '</div>';
    }
  }).join('');
}

function renderTestResultsFromEvents(results) {
  renderResultsFromEvents(results, {
    tabId: 'left-tab-testing',
    listId: 'modal-test-results-list',
    entryPrefix: 'test-result-entry-',
    autoSwitch: results && results.length > 0,
  });
}

// --- Diff helpers ---

function parseDiffByFile(diff) {
  const files = [];
  // Track current workspace section from "=== name ===" separators.
  let currentWorkspace = '';
  const blocks = diff.split(/(?=^diff --git )/m);
  for (const block of blocks) {
    if (!block.trim()) continue;
    const lines = block.split('\n');
    // Extract workspace separator if present (before or after diff content).
    for (const line of lines) {
      const wsMatch = line.match(/^=== (.+) ===$/);
      if (wsMatch) currentWorkspace = wsMatch[1];
    }
    const match = lines[0].match(/^diff --git a\/.+ b\/(.+)$/);
    if (!match) continue; // skip blocks without diff header (e.g. bare separators)
    const filename = match[1];
    let adds = 0, dels = 0;
    for (const line of lines.slice(1)) {
      if (line.startsWith('+') && !line.startsWith('+++')) adds++;
      if (line.startsWith('-') && !line.startsWith('---')) dels++;
    }
    files.push({ filename, content: block, adds, dels, workspace: currentWorkspace });
  }
  return files;
}

function renderDiffLine(line) {
  const escaped = escapeHtml(line);
  if (line.startsWith('+') && !line.startsWith('+++')) return `<span class="diff-line diff-add">${escaped}</span>`;
  if (line.startsWith('-') && !line.startsWith('---')) return `<span class="diff-line diff-del">${escaped}</span>`;
  if (line.startsWith('@@')) return `<span class="diff-line diff-hunk">${escaped}</span>`;
  if (/^(diff |--- |\+{3} |index |Binary )/.test(line)) return `<span class="diff-line diff-header">${escaped}</span>`;
  return `<span class="diff-line">${escaped}</span>`;
}

function renderDiffFiles(container, diff) {
  if (!diff) {
    container.innerHTML = '<span class="text-xs text-v-muted">No changes</span>';
    return;
  }
  const files = parseDiffByFile(diff);
  if (files.length === 0) {
    container.innerHTML = '<span class="text-xs text-v-muted">No changes</span>';
    return;
  }
  let lastWorkspace = '';
  container.innerHTML = files.map(f => {
    let wsHeader = '';
    if (f.workspace && f.workspace !== lastWorkspace) {
      lastWorkspace = f.workspace;
      wsHeader = `<div class="diff-workspace-header">${escapeHtml(f.workspace)}</div>`;
    }
    const statsHtml = [
      f.adds > 0 ? `<span class="diff-add">+${f.adds}</span>` : '',
      f.dels > 0 ? `<span class="diff-del">&minus;${f.dels}</span>` : '',
    ].filter(Boolean).join(' ');
    const diffHtml = f.content.split('\n').filter(l => !/^=== .+ ===$/.test(l)).map(renderDiffLine).join('\n');
    return wsHeader + `<details class="diff-file">
      <summary class="diff-file-summary">
        <span class="diff-filename">${escapeHtml(f.filename)}</span>
        <span class="diff-stats">${statsHtml}</span>
      </summary>
      <pre class="diff-block diff-block-modal">${diffHtml}</pre>
    </details>`;
  }).join('');
}

// --- Modal ---

async function openModal(id) {
  currentTaskId = id;
  const task = tasks.find(t => t.id === id);
  if (!task) return;

  document.getElementById('modal-badge').className = `badge badge-${task.status}`;
  document.getElementById('modal-badge').textContent = task.status === 'in_progress' ? 'in progress' : task.status;
  document.getElementById('modal-time').textContent = new Date(task.created_at).toLocaleString();
  document.getElementById('modal-id').textContent = `ID: ${task.id}`;
  const displayPrompt = (typeof taskDisplayPrompt === 'function') ? taskDisplayPrompt(task) : (task.prompt || '');

  const backlogRight = document.getElementById('modal-backlog-right');
  if (task.status === 'backlog') {
    // Left panel: rendered prompt
    const promptRaw = document.getElementById('modal-prompt');
    const promptRendered = document.getElementById('modal-prompt-rendered');
    promptRaw.textContent = displayPrompt;
    promptRendered.innerHTML = renderMarkdown(displayPrompt);
    promptRendered.classList.remove('hidden');
    promptRaw.classList.add('hidden');
    document.getElementById('modal-prompt-actions').classList.remove('hidden');
    document.getElementById('toggle-prompt-btn').textContent = 'Raw';

    // Right panel: settings + edit + refinement
    backlogRight.classList.remove('hidden');
    document.getElementById('modal-edit-prompt').value = task.prompt;
    document.getElementById('modal-edit-timeout').value = String(task.timeout || 60);
    document.getElementById('modal-edit-mount-worktrees').checked = !!task.mount_worktrees;
    document.getElementById('modal-edit-sandbox').value = task.sandbox || '';
    const resumeRow = document.getElementById('modal-edit-resume-row');
    if (task.session_id) {
      resumeRow.classList.remove('hidden');
      document.getElementById('modal-edit-resume').checked = !task.fresh_start;
    } else {
      resumeRow.classList.add('hidden');
    }

    // Reset and render refinement panel
    resetRefinePanel();
    updateRefineUI(task);
    renderRefineHistory(task);

    // Make modal wide for split layout
    const modalCard = document.querySelector('#modal .modal-card');
    modalCard.classList.add('modal-wide');
    const modalBody = document.getElementById('modal-body');
    modalBody.style.display = 'flex';
    modalBody.style.gap = '0';
  } else {
    backlogRight.classList.add('hidden');
    const promptRaw = document.getElementById('modal-prompt');
    const promptRendered = document.getElementById('modal-prompt-rendered');
    promptRaw.textContent = displayPrompt;
    promptRendered.innerHTML = renderMarkdown(displayPrompt);
    promptRendered.classList.remove('hidden');
    promptRaw.classList.add('hidden');
    document.getElementById('modal-prompt-actions').classList.remove('hidden');
    document.getElementById('toggle-prompt-btn').textContent = 'Raw';
  }

  // Reset left panel tabs; content populated below once events load
  document.getElementById('left-tab-testing').classList.add('hidden');
  setLeftTab('implementation');
  if (task.result) {
    renderResultsFromEvents([task.result]);
  } else {
    document.getElementById('modal-summary-section').classList.add('hidden');
  }

  // Usage stats (show when any tokens have been used)
  const usageSection = document.getElementById('modal-usage-section');
  const u = task.usage;
  if (u && (u.input_tokens || u.output_tokens || u.cost_usd)) {
    document.getElementById('modal-usage-input').textContent = u.input_tokens.toLocaleString();
    document.getElementById('modal-usage-output').textContent = u.output_tokens.toLocaleString();
    document.getElementById('modal-usage-cache-read').textContent = u.cache_read_input_tokens.toLocaleString();
    document.getElementById('modal-usage-cache-creation').textContent = u.cache_creation_input_tokens.toLocaleString();
    document.getElementById('modal-usage-cost').textContent = '$' + u.cost_usd.toFixed(4);
    usageSection.classList.remove('hidden');

    // Per-sub-agent breakdown
    const breakdownEl = document.getElementById('modal-usage-breakdown');
    const breakdownRows = document.getElementById('modal-usage-breakdown-rows');
    const bd = task.usage_breakdown;
    if (bd && Object.keys(bd).length > 0) {
      // Display order for known agents; unknown agents appended after.
      const order = ['implementation', 'test', 'refinement', 'title', 'oversight', 'oversight-test'];
      const agentLabel = {
        'implementation': 'Implementation',
        'test': 'Test verification',
        'refinement': 'Refinement',
        'title': 'Title generation',
        'oversight': 'Impl. oversight',
        'oversight-test': 'Test oversight',
      };
      const keys = [
        ...order.filter(k => bd[k]),
        ...Object.keys(bd).filter(k => !order.includes(k)),
      ];
      breakdownRows.innerHTML = '';
      keys.forEach(function(agent) {
        const au = bd[agent];
        if (!au) return;
        const label = agentLabel[agent] || agent;
        const row = document.createElement('div');
        row.style.cssText = 'display:flex;justify-content:space-between;align-items:baseline;font-size:12px;padding:2px 0;';
        const nameSpan = document.createElement('span');
        nameSpan.style.color = 'var(--text-muted)';
        nameSpan.textContent = label;
        const statsSpan = document.createElement('span');
        statsSpan.style.cssText = 'font-family:monospace;font-size:11px;color:var(--text-secondary);';
        const parts = [];
        if (au.input_tokens || au.output_tokens) {
          parts.push((au.input_tokens || 0).toLocaleString() + ' in / ' + (au.output_tokens || 0).toLocaleString() + ' out');
        }
        if (au.cost_usd) {
          parts.push('$' + au.cost_usd.toFixed(4));
        } else if (au.input_tokens || au.output_tokens) {
          parts.push('(tokens only)');
        }
        statsSpan.textContent = parts.join(' · ');
        row.appendChild(nameSpan);
        row.appendChild(statsSpan);
        breakdownRows.appendChild(row);
      });
      breakdownEl.classList.remove('hidden');
    } else {
      breakdownEl.classList.add('hidden');
    }
  } else {
    usageSection.classList.add('hidden');
  }

  const feedbackSection = document.getElementById('modal-feedback-section');
  feedbackSection.classList.toggle('hidden', task.status !== 'waiting');
  // Reset test sub-section each time the modal opens.
  document.getElementById('modal-test-section').classList.add('hidden');
  document.getElementById('modal-test-criteria').value = '';

  // Right panel setup
  const modalCard = document.querySelector('#modal .modal-card');
  const modalRight = document.getElementById('modal-right');
  const hasWorktrees = task.worktree_paths && Object.keys(task.worktree_paths).length > 0;
  // Hide test button when there are no worktrees (no changes produced); refined after diff loads.
  const testBtn = document.getElementById('modal-test-btn');
  if (testBtn) testBtn.classList.toggle('hidden', !hasWorktrees);
  const modalBody = document.getElementById('modal-body');

  if (task.status !== 'backlog') {
    modalCard.classList.add('modal-wide');
    modalRight.classList.remove('hidden');
    modalBody.style.display = 'flex';
    modalBody.style.gap = '0';

    // Start log streaming; show Testing tab when test data exists
    if (task.is_test_run || task.last_test_result) {
      // Shown both while the test is running (is_test_run) and after it
      // completes (last_test_result set, is_test_run cleared), so done/verified
      // tasks still expose test traces.
      const testTab = document.getElementById('right-tab-testing');
      if (testTab) testTab.classList.remove('hidden');
      startImplLogFetch(id);
      startTestLogStream(id);
    } else {
      const testTab = document.getElementById('right-tab-testing');
      if (testTab) testTab.classList.add('hidden');
      startLogStream(id);
    }

    // Changes tab: show for waiting/failed/done tasks with worktrees
    const changesTab = document.getElementById('right-tab-changes');
    const isTestTaskCard = task.status === 'waiting' && !!task.last_test_result && task.test_run_start_turn > 0;
    if ((task.status === 'failed' || task.status === 'done' || (task.status === 'waiting' && !isTestTaskCard)) && hasWorktrees) {
      if (changesTab) changesTab.classList.remove('hidden');
      const filesEl = document.getElementById('modal-diff-files');
      const behindEl = document.getElementById('modal-diff-behind');
      filesEl.innerHTML = '<span class="text-xs text-v-muted">Loading diff\u2026</span>';
      if (behindEl) behindEl.classList.add('hidden');
      api(`/api/tasks/${task.id}/diff`).then(data => {
        const el = document.getElementById('modal-diff-files');
        if (el) renderDiffFiles(el, data.diff);
        // Hide test button when diff is empty (task produced no changes).
        const testBtn = document.getElementById('modal-test-btn');
        if (testBtn) testBtn.classList.toggle('hidden', !data.diff);
        const behindCounts = data.behind_counts || {};
        const entries = Object.entries(behindCounts);
        const totalBehind = entries.reduce((s, [, n]) => s + n, 0);
        const warnEl = document.getElementById('modal-diff-behind');
        if (warnEl) {
          if (!isTestTaskCard && totalBehind > 0) {
            const label = entries.length === 1
              ? `${totalBehind} commit${totalBehind !== 1 ? 's' : ''} behind`
              : entries.map(([repo, n]) => `${repo}: ${n}`).join(', ') + ' behind';
            warnEl.innerHTML =
              `<span>\u26a0 ${escapeHtml(label)}</span>` +
              `<button class="diff-sync-btn" onclick="syncTask('${task.id}');closeModal()">Sync with latest</button>`;
            warnEl.classList.remove('hidden');
          } else {
            warnEl.classList.add('hidden');
          }
        }
      }).catch(() => {
        const el = document.getElementById('modal-diff-files');
        if (el) el.innerHTML = '<span class="text-xs ev-error">Failed to load diff</span>';
      });
    } else {
      if (changesTab) changesTab.classList.add('hidden');
    }

    // Default to Implementation tab
    setRightTab('implementation');
  } else {
    modalCard.classList.remove('modal-wide');
    modalRight.classList.add('hidden');
    modalBody.style.display = '';
    modalBody.style.gap = '';
  }

  // Resume section (failed with session_id only)
  const resumeSection = document.getElementById('modal-resume-section');
  if (task.status === 'failed' && task.session_id) {
    resumeSection.classList.remove('hidden');
    const resumeTimeoutEl = document.getElementById('modal-resume-timeout');
    if (resumeTimeoutEl) {
      resumeTimeoutEl.value = String(task.timeout || DEFAULT_TASK_TIMEOUT);
    }
  } else {
    resumeSection.classList.add('hidden');
  }

  // Cancel section (backlog / in_progress / waiting / failed)
  const cancelSection = document.getElementById('modal-cancel-section');
  const cancellable = ['backlog', 'in_progress', 'waiting', 'failed'];
  cancelSection.classList.toggle('hidden', !cancellable.includes(task.status));

  // Retry section (done / failed / waiting / cancelled)
  const retrySection = document.getElementById('modal-retry-section');
  const retryResumeRow = document.getElementById('modal-retry-resume-row');
  if (task.status === 'done' || task.status === 'failed' || task.status === 'waiting' || task.status === 'cancelled') {
    retrySection.classList.remove('hidden');
    document.getElementById('modal-retry-prompt').value = task.prompt;
    if (task.session_id) {
      retryResumeRow.classList.remove('hidden');
      document.getElementById('modal-retry-resume').checked = !task.fresh_start;
    } else {
      retryResumeRow.classList.add('hidden');
    }
  } else {
    retrySection.classList.add('hidden');
    retryResumeRow.classList.add('hidden');
  }

  // Archive/Unarchive section (done or cancelled tasks)
  const archiveSection = document.getElementById('modal-archive-section');
  const unarchiveSection = document.getElementById('modal-unarchive-section');
  const isArchivable = task.status === 'done' || task.status === 'cancelled';
  if (isArchivable && !task.archived) {
    archiveSection.classList.remove('hidden');
    unarchiveSection.classList.add('hidden');
  } else if (isArchivable && task.archived) {
    archiveSection.classList.add('hidden');
    unarchiveSection.classList.remove('hidden');
  } else {
    archiveSection.classList.add('hidden');
    unarchiveSection.classList.add('hidden');
  }

  // Prompt history
  const historySection = document.getElementById('modal-history-section');
  if (task.prompt_history && task.prompt_history.length > 0) {
    historySection.classList.remove('hidden');
    const historyList = document.getElementById('modal-history-list');
    historyList.innerHTML = task.prompt_history.map((p, i) =>
      `<pre class="code-block text-xs" style="opacity:0.7;border:1px solid var(--border);"><span class="text-v-muted" style="font-size:10px;">#${i + 1}</span>\n${escapeHtml(p)}</pre>`
    ).join('');
  } else {
    historySection.classList.add('hidden');
  }

  // Load events
  try {
    const events = await api(`/api/tasks/${id}/events`);

    // Replace single-result fallback with all turn results from output events.
    // When a test run has occurred, split output events at the test boundary so
    // implementation and test agent results are shown in separate sections.
    const outputResults = events
      .filter(e => e.event_type === 'output' && e.data && e.data.result)
      .map(e => e.data.result);
    const testStartTurn = task.test_run_start_turn || 0;
    const implResults = testStartTurn > 0 ? outputResults.slice(0, testStartTurn) : outputResults;
    const testResults = testStartTurn > 0 ? outputResults.slice(testStartTurn) : [];
    if (implResults.length > 0) {
      renderResultsFromEvents(implResults);
    }
    renderTestResultsFromEvents(testResults);

    const container = document.getElementById('modal-events');
    container.innerHTML = events.map(e => {
      const time = new Date(e.created_at).toLocaleTimeString();
      let detail = '';
      const data = e.data || {};
      if (e.event_type === 'state_change') {
        detail = `${data.from || '(new)'} → ${data.to}`;
      } else if (e.event_type === 'feedback') {
        detail = `"${escapeHtml(data.message)}"`;
      } else if (e.event_type === 'output') {
        detail = `stop_reason: ${data.stop_reason || '(none)'}`;
      } else if (e.event_type === 'system') {
        detail = escapeHtml(data.result || '');
      } else if (e.event_type === 'error') {
        detail = escapeHtml(data.error);
      }
      const typeClasses = {
        state_change: 'ev-state',
        output: 'ev-output',
        system: 'ev-system',
        feedback: 'ev-feedback',
        error: 'ev-error',
      };
      return `<div class="flex items-start gap-2 text-xs">
        <span class="text-v-muted shrink-0">${time}</span>
        <span class="${typeClasses[e.event_type] || 'text-v-muted'} shrink-0">${e.event_type}</span>
        <span class="text-v-secondary">${detail}</span>
      </div>`;
    }).join('');
  } catch (e) {
    document.getElementById('modal-events').innerHTML = '<span class="text-xs ev-error">Failed to load events</span>';
  }

  document.getElementById('modal').classList.remove('hidden');
  document.getElementById('modal').classList.add('flex');
}

function closeModal() {
  if (logsAbort) {
    logsAbort.abort();
    logsAbort = null;
  }
  if (testLogsAbort) {
    testLogsAbort.abort();
    testLogsAbort = null;
  }
  rawLogBuffer = '';
  testRawLogBuffer = '';
  oversightData = null;
  oversightFetching = false;
  logsMode = 'oversight';
  document.getElementById('modal-logs').innerHTML = '';
  document.getElementById('modal-test-logs').innerHTML = '';
  resetRefinePanel();
  document.getElementById('modal-backlog-right').classList.add('hidden');
  currentTaskId = null;
  document.querySelector('#modal .modal-card').classList.remove('modal-wide');
  const modalBody = document.getElementById('modal-body');
  modalBody.style.display = '';
  modalBody.style.gap = '';
  document.getElementById('modal').classList.add('hidden');
  document.getElementById('modal').classList.remove('flex');
}

// ANSI foreground colors tuned for the dark (#0d1117) terminal background.
const ANSI_FG = ['#484f58','#ff7b72','#3fb950','#e3b341','#79c0ff','#ff79c6','#39c5cf','#b1bac4'];
const ANSI_FG_BRIGHT = ['#6e7681','#ffa198','#56d364','#f8e3ad','#cae8ff','#fecfe8','#b3f0ff','#ffffff'];

// Convert ANSI escape codes to HTML <span> tags.
// Carriage returns are collapsed so only the last overwrite per line is shown,
// matching how a real terminal renders spinner animations.
function ansiToHtml(rawText) {
  const lines = rawText.split('\n');
  const text = lines.map(line => {
    const parts = line.split('\r');
    return parts[parts.length - 1];
  }).join('\n');

  const seqRegex = /\x1b\[([0-9;]*)([A-Za-z])/g;
  let result = '';
  let lastIndex = 0;
  let openSpans = 0;
  let match;

  function esc(s) {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }

  while ((match = seqRegex.exec(text)) !== null) {
    if (match.index > lastIndex) result += esc(text.slice(lastIndex, match.index));
    lastIndex = seqRegex.lastIndex;

    if (match[2] === 'm') {
      while (openSpans > 0) { result += '</span>'; openSpans--; }
      const codes = match[1] ? match[1].split(';').map(Number) : [0];
      let style = '';
      let i = 0;
      while (i < codes.length) {
        const c = codes[i];
        if (c === 1) style += 'font-weight:bold;';
        else if (c === 2) style += 'opacity:0.6;';
        else if (c === 3) style += 'font-style:italic;';
        else if (c === 4) style += 'text-decoration:underline;';
        else if (c >= 30 && c <= 37) style += `color:${ANSI_FG[c - 30]};`;
        else if (c >= 90 && c <= 97) style += `color:${ANSI_FG_BRIGHT[c - 90]};`;
        else if (c === 38 && codes[i + 1] === 2 && i + 4 < codes.length) {
          style += `color:rgb(${codes[i + 2]},${codes[i + 3]},${codes[i + 4]});`;
          i += 4;
        }
        i++;
      }
      if (style) { result += `<span style="${style}">`; openSpans++; }
    }
    // Other ANSI commands (cursor movement, erase-line, etc.) are intentionally ignored.
  }

  if (lastIndex < text.length) result += esc(text.slice(lastIndex));
  while (openSpans > 0) { result += '</span>'; openSpans--; }
  return result;
}

// --- Pretty NDJSON rendering (Claude Code terminal style) ---

function parseNdjsonLine(line) {
  const t = line.trim();
  if (t.length === 0 || t[0] !== '{') return null;
  try { return JSON.parse(t); } catch { return null; }
}

function extractToolInput(name, inputObj) {
  if (!inputObj || typeof inputObj !== 'object') return '';
  switch (name) {
    case 'Bash': return inputObj.command || '';
    case 'Read': return inputObj.file_path || '';
    case 'Write': return inputObj.file_path || '';
    case 'Edit': return inputObj.file_path || '';
    case 'Glob': return inputObj.pattern || '';
    case 'Grep': return inputObj.pattern || '';
    case 'WebFetch': return inputObj.url || '';
    case 'WebSearch': return inputObj.query || '';
    case 'Task': return inputObj.prompt ? inputObj.prompt.slice(0, 120) : '';
    case 'TodoWrite': return inputObj.todos ? `${inputObj.todos.length} items` : '';
    default: {
      // Try common keys
      for (const key of ['file_path', 'command', 'pattern', 'query', 'path']) {
        if (inputObj[key]) return String(inputObj[key]);
      }
      return '';
    }
  }
}

function renderPrettyLogs(rawBuffer) {
  const lines = rawBuffer.split('\n');
  const blocks = [];

  for (const line of lines) {
    const evt = parseNdjsonLine(line);
    if (!evt) {
      // Non-JSON line (stderr progress output) — render with ANSI colors.
      const trimmed = line.trim();
      if (trimmed) {
        blocks.push(`<div class="cc-block cc-stderr">${ansiToHtml(line)}</div>`);
      }
      continue;
    }

    if (evt.type === 'assistant' && evt.message && evt.message.content) {
      for (const block of evt.message.content) {
        if (block.type === 'text' && block.text) {
          blocks.push(`<div class="cc-block cc-text"><span class="cc-marker">&#x23FA;</span> ${escapeHtml(block.text)}</div>`);
        } else if (block.type === 'tool_use') {
          let input = '';
          if (block.input) {
            const parsed = typeof block.input === 'string' ? (() => { try { return JSON.parse(block.input); } catch { return null; } })() : block.input;
            input = parsed ? extractToolInput(block.name, parsed) : '';
          }
          const inputHtml = input ? `(<span class="cc-tool-input">${escapeHtml(input.length > 200 ? input.slice(0, 200) + '\u2026' : input)}</span>)` : '';
          blocks.push(`<div class="cc-block cc-tool-call"><span class="cc-marker">&#x23FA;</span> <span class="cc-tool-name">${escapeHtml(block.name)}</span>${inputHtml}</div>`);
        }
      }
    } else if (evt.type === 'user' && evt.message && evt.message.content) {
      for (const block of evt.message.content) {
        if (block.type !== 'tool_result') continue;
        let text = '';
        if (Array.isArray(block.content)) {
          for (const c of block.content) {
            if (c.text) text += c.text;
          }
        } else if (typeof block.content === 'string') {
          text = block.content;
        }
        if (!text) {
          blocks.push(`<div class="cc-block cc-tool-result"><span class="cc-result-pipe">&#x23BF;</span> <span class="cc-result-empty">(No output)</span></div>`);
          continue;
        }
        // Clean Read tool output: "   123→\tcode" → "   123  code"
        text = text.replace(/^(\s*\d+)→\t?/gm, '$1  ');
        const resultLines = text.split('\n');
        if (resultLines.length > 5) {
          const preview = resultLines.slice(0, 3).map(l => escapeHtml(l)).join('\n');
          const rest = resultLines.slice(3).map(l => escapeHtml(l)).join('\n');
          const remaining = resultLines.length - 3;
          blocks.push(`<div class="cc-block cc-tool-result"><span class="cc-result-pipe">&#x23BF;</span> <pre class="cc-result-text">${preview}</pre><details class="cc-expand"><summary class="cc-expand-toggle">+${remaining} lines</summary><pre class="cc-result-text">${rest}</pre></details></div>`);
        } else {
          blocks.push(`<div class="cc-block cc-tool-result"><span class="cc-result-pipe">&#x23BF;</span> <pre class="cc-result-text">${escapeHtml(text)}</pre></div>`);
        }
      }
    } else if (evt.type === 'result') {
      if (evt.result) {
        blocks.push(`<div class="cc-block cc-final-result"><span class="cc-marker cc-marker-result">&#x23FA;</span> <span class="cc-result-label">[Result]</span> ${escapeHtml(evt.result)}</div>`);
      }
    }
  }

  return blocks.join('');
}

// --- Oversight view ---

// oversightData caches the last fetched oversight for the open task.
// Cleared when the modal opens a different task.
let oversightData = null;
let oversightFetching = false;

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

// testOversightData caches the last fetched test oversight for the open task.
let testOversightData = null;
let testOversightFetching = false;

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
