// --- Modal lifecycle ---

async function openModal(id) {
  currentTaskId = id;
  const task = tasks.find(t => t.id === id);
  if (!task) return;

  document.getElementById('modal-badge').className = `badge badge-${task.status}`;
  document.getElementById('modal-badge').textContent = task.status === 'in_progress' ? 'in progress' : task.status;
  document.getElementById('modal-time').textContent = new Date(task.created_at).toLocaleString();
  document.getElementById('modal-id').textContent = `ID: ${task.id}`;

  const backlogRight = document.getElementById('modal-backlog-right');
  if (task.status === 'backlog') {
    // Left panel: rendered prompt
    const promptRaw = document.getElementById('modal-prompt');
    const promptRendered = document.getElementById('modal-prompt-rendered');
    promptRaw.textContent = task.prompt;
    promptRendered.innerHTML = renderMarkdown(task.prompt);
    promptRendered.classList.remove('hidden');
    promptRaw.classList.add('hidden');
    document.getElementById('modal-prompt-actions').classList.remove('hidden');
    document.getElementById('toggle-prompt-btn').textContent = 'Raw';

    // Right panel: settings + edit + refinement
    backlogRight.classList.remove('hidden');
    document.getElementById('modal-edit-prompt').value = task.prompt;
    document.getElementById('modal-edit-timeout').value = String(task.timeout || 60);
    document.getElementById('modal-edit-mount-worktrees').checked = !!task.mount_worktrees;
    document.getElementById('modal-edit-model').value = task.model || '';
    const resumeRow = document.getElementById('modal-edit-resume-row');
    if (task.session_id) {
      resumeRow.classList.remove('hidden');
      document.getElementById('modal-edit-resume').checked = !task.fresh_start;
    } else {
      resumeRow.classList.add('hidden');
    }

    // Reset refinement chat state
    resetRefinePanel();
    document.getElementById('refine-start-btn').classList.remove('hidden');
    document.getElementById('refine-chat-area').classList.add('hidden');

    // Populate refinement history
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
    promptRaw.textContent = task.prompt;
    promptRendered.innerHTML = renderMarkdown(task.prompt);
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
    if ((task.status === 'waiting' || task.status === 'failed' || task.status === 'done') && hasWorktrees) {
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
          if (totalBehind > 0) {
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
