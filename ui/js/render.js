// Brainstorm category values — must mirror ideaCategoryPool in internal/runner/ideate.go.
// Tags matching these strings are rendered with the badge-category style so
// that category of a brainstorm-generated task card is visually distinct.
const BRAINSTORM_CATEGORIES = new Set([
  'product feature',
  'frontend / UX',
  'backend / API',
  'performance optimization',
  'code quality / refactoring',
  'test coverage',
  'developer experience',
  'security hardening',
  'observability / debugging',
  'infrastructure / ops',
  'data model / storage',
]);

// --- Dependency badge helpers ---

function areDepsBlocked(t) {
  if (!t.depends_on || t.depends_on.length === 0) return false;
  return t.depends_on.some(function(depId) {
    var dep = tasks.find(function(d) { return d.id === depId; });
    return !dep || dep.status !== 'done';
  });
}

function getBlockingTaskNames(t) {
  if (!t.depends_on) return '';
  return t.depends_on.map(function(id) {
    var dep = tasks.find(function(d) { return d.id === id; });
    if (!dep) return id.slice(0, 8) + '\u2026';
    return dep.title || (dep.prompt.length > 30 ? dep.prompt.slice(0, 30) + '\u2026' : dep.prompt);
  }).join(', ');
}

// --- Board rendering ---

function formatInProgressCount(count) {
  return '' + count;
}

function updateMaxParallelTag() {
  const tag = document.getElementById('max-parallel-tag');
  if (!tag) return;
  if (maxParallelTasks > 0) {
    tag.textContent = 'max ' + maxParallelTasks;
    tag.classList.remove('hidden');
  } else {
    tag.classList.add('hidden');
  }
}

function updateInProgressCount() {
  const countEl = document.getElementById('count-in_progress');
  if (!countEl) return;
  const col = document.getElementById('col-in_progress');
  const current = col ? col.children.length : 0;
  countEl.textContent = formatInProgressCount(current);
  updateMaxParallelTag();
}

const diffCache = new Map(); // taskId -> {diff: string, behindCounts, updatedAt, behindFetchedAt} | 'loading'

// Invalidate cached behind-counts for all tasks so that the next render re-checks
// how many commits each waiting card is behind. Called whenever any task changes.
function invalidateDiffBehindCounts() {
  for (const [, cached] of diffCache) {
    if (cached && cached !== 'loading') {
      cached.behindFetchedAt = 0;
    }
  }
}

function renderDiffInto(el, diff) {
  if (!diff) {
    el.innerHTML = '<span style="color:var(--text-muted)">no changes</span>';
    return;
  }
  const lines = diff.split('\n');
  el.innerHTML = lines.map(line => {
    const escaped = escapeHtml(line);
    if (/^=== .+ ===$/.test(line)) {
      return `<span class="diff-workspace-label">${escaped}</span>`;
    } else if (line.startsWith('+') && !line.startsWith('+++')) {
      return `<span class="diff-add">${escaped}</span>`;
    } else if (line.startsWith('-') && !line.startsWith('---')) {
      return `<span class="diff-del">${escaped}</span>`;
    } else if (line.startsWith('@@')) {
      return `<span class="diff-hunk">${escaped}</span>`;
    } else if (line.startsWith('diff ') || line.startsWith('--- ') || line.startsWith('+++ ') || line.startsWith('index ') || line.startsWith('Binary ')) {
      return `<span class="diff-header">${escaped}</span>`;
    }
    return escaped;
  }).join('\n');
}

async function fetchDiff(card, taskId, updatedAt) {
  const cached = diffCache.get(taskId);
  if (cached === 'loading') return;
  // Cache is valid if the task hasn't changed AND behind-counts were freshly checked.
  // behindFetchedAt is zeroed by invalidateDiffBehindCounts() whenever any task changes.
  if (cached && cached.updatedAt === updatedAt && cached.behindFetchedAt) {
    const diffEl = card.querySelector('[data-diff]');
    if (diffEl) applyDiffToCard(diffEl, cached.diff, cached.behindCounts, taskId);
    return;
  }
  diffCache.set(taskId, 'loading');
  try {
    const data = await api(`/api/tasks/${taskId}/diff`);
    const behindCounts = data.behind_counts || {};
    diffCache.set(taskId, { diff: data.diff, behindCounts, updatedAt, behindFetchedAt: Date.now() });
    const latestEl = card.querySelector('[data-diff]');
    if (latestEl) applyDiffToCard(latestEl, data.diff, behindCounts, taskId);
  } catch {
    diffCache.delete(taskId);
  }
}

function applyDiffToCard(el, diff, behindCounts, taskId) {
  const entries = Object.entries(behindCounts || {});
  const totalBehind = entries.reduce((s, [, n]) => s + n, 0);
  let warning = '';
  if (totalBehind > 0) {
    const label = entries.length === 1
      ? `${totalBehind} commit${totalBehind !== 1 ? 's' : ''} behind`
      : entries.map(([repo, n]) => `${repo}: ${n}`).join(', ') + ' behind';
    warning = `<div class="diff-behind-warning">` +
      `<span>\u26a0 ${escapeHtml(label)}</span>` +
      `<button class="diff-sync-btn" onclick="event.stopPropagation();syncTask('${taskId}')">Sync</button>` +
      `</div>`;
  }
  const tmp = document.createElement('div');
  renderDiffInto(tmp, diff);
  el.innerHTML = warning + tmp.innerHTML;
}

function render() {
  // Sync ideation spinner from live task list (no polling needed).
  if (typeof updateIdeationFromTasks === 'function') updateIdeationFromTasks(tasks);

  const columns = { backlog: [], in_progress: [], waiting: [], committing: [], done: [], failed: [], cancelled: [] };
  for (const t of tasks) {
    const col = columns[t.status];
    if (col) col.push(t);
  }

  // Failed and committing tasks show in the Waiting column.
  // Failed tasks are visually distinguished by a red left border on the card.
  columns.waiting = columns.waiting.concat(columns.failed).concat(columns.committing);
  delete columns.committing;
  delete columns.failed;

  // Cancelled tasks show in the Done column.
  // Cancelled tasks are visually distinguished by a purple left border on the card.
  columns.done = columns.done.concat(columns.cancelled);
  delete columns.cancelled;

  for (const [status, items] of Object.entries(columns)) {
    const el = document.getElementById(`col-${status}`);
    if (!el) continue;

    // Backlog: sort by position ascending (priority order).
    // Other columns: sort by last updated descending.
    if (status === 'backlog') {
      items.sort((a, b) => a.position - b.position);
    } else {
      items.sort((a, b) => new Date(b.updated_at) - new Date(a.updated_at));
    }

    // Apply search filter: only show cards matching the current query.
    const visibleItems = filterQuery ? items.filter(matchesFilter) : items;

    const countEl = document.getElementById(`count-${status}`);
    if (countEl) {
      const isFiltered = filterQuery && visibleItems.length !== items.length;
      if (status === 'in_progress') {
        countEl.textContent = isFiltered
          ? formatInProgressCount(visibleItems.length) + '\u00a0/\u00a0' + items.length
          : formatInProgressCount(items.length);
        updateMaxParallelTag();
      } else {
        countEl.textContent = isFiltered
          ? visibleItems.length + '\u00a0/\u00a0' + items.length
          : items.length;
      }
    }

    const existing = new Map();
    for (const child of el.children) {
      existing.set(child.dataset.id, child);
    }

    const newIds = new Set(visibleItems.map(t => t.id));

    // Remove cards that are no longer in this column or hidden by the filter.
    for (const [id, child] of existing) {
      if (!newIds.has(id)) child.remove();
    }

    // Add or update visible cards, maintaining sorted order in the DOM.
    for (let i = 0; i < visibleItems.length; i++) {
      const t = visibleItems[i];
      let card = existing.get(t.id);
      const rank = status === 'backlog' ? i : undefined;
      if (!card) {
        card = createCard(t, rank);
      } else {
        updateCard(card, t, rank);
      }
      if (el.children[i] !== card) {
        el.insertBefore(card, el.children[i] || null);
      }
      // Load diff for waiting/failed/done tasks that have worktrees
      if ((t.status === 'waiting' || t.status === 'failed' || t.status === 'done') && t.worktree_paths && Object.keys(t.worktree_paths).length > 0) {
        fetchDiff(card, t.id, t.updated_at);
      }
    }
  }

  // Update done column usage stats
  const doneStatsEl = document.getElementById('done-stats');
  if (doneStatsEl) {
    const doneItems = columns.done || [];
    const totalInput = doneItems.reduce(function(s, t) { return s + (t.usage && t.usage.input_tokens || 0); }, 0);
    const totalOutput = doneItems.reduce(function(s, t) { return s + (t.usage && t.usage.output_tokens || 0); }, 0);
    const totalCost = doneItems.reduce(function(s, t) { return s + (t.usage && t.usage.cost_usd || 0); }, 0);
    if (totalInput || totalOutput || totalCost) {
      doneStatsEl.textContent = totalInput.toLocaleString() + ' in / ' + totalOutput.toLocaleString() + ' out / $' + totalCost.toFixed(4);
      doneStatsEl.classList.remove('hidden');
    } else {
      doneStatsEl.classList.add('hidden');
    }
  }

  // Show/hide "Archive all" button based on whether there are non-archived done tasks
  const archiveAllBtn = document.getElementById('archive-all-btn');
  if (archiveAllBtn) {
    const hasDone = (columns.done || []).some(function(t) { return !t.archived; });
    archiveAllBtn.classList.toggle('hidden', !hasDone);
  }

  // If the modal is open for a backlog task, refresh its refinement panel
  // so live sandbox status updates are reflected without reopening the modal.
  if (currentTaskId) {
    const openTask = tasks.find(t => t.id === currentTaskId);
    if (openTask && openTask.status === 'backlog') {
      updateRefineUI(openTask);
      renderRefineHistory(openTask);
    }
  }

  if (window.depGraphEnabled && typeof renderDependencyGraph === 'function') renderDependencyGraph(tasks);
  else if (typeof hideDependencyGraph === 'function') hideDependencyGraph();
}

function createCard(t, rank) {
  const card = document.createElement('div');
  card.className = 'card';
  card.dataset.id = t.id;
  card.dataset.taskId = t.id;
  card.onclick = () => openModal(t.id);
  updateCard(card, t, rank);
  return card;
}

function buildCardActions(t) {
  if (t.archived) return '';
  const parts = [];
  if (t.status === 'backlog') {
    const refineStatus = t.current_refinement && t.current_refinement.status;
    const refineBlocked = refineStatus === 'running' || refineStatus === 'done';
    const refineTitle = refineStatus === 'running' ? 'Refinement in progress' : refineStatus === 'done' ? 'Review the refined prompt before starting' : '';
    parts.push(`<button class="card-action-btn card-action-refine" onclick="event.stopPropagation();openModal('${t.id}').then(()=>startRefinement())" title="Refine task with AI">&#9998; Refine</button>`);
    parts.push(`<button class="card-action-btn card-action-start" ${refineBlocked ? `disabled title="${refineTitle}"` : `onclick="event.stopPropagation();updateTaskStatus('${t.id}','in_progress')" title="Move to In Progress"`}>&#9654; Start</button>`);
  } else if (t.status === 'waiting') {
    parts.push(`<button class="card-action-btn card-action-test" onclick="event.stopPropagation();quickTestTask('${t.id}')" title="Run test agent">&#9654; Test</button>`);
    parts.push(`<button class="card-action-btn card-action-done" onclick="event.stopPropagation();quickDoneTask('${t.id}')" title="Mark done and commit">&#10003; Done</button>`);
  } else if (t.status === 'failed') {
    if (t.session_id) {
      parts.push(`<button class="card-action-btn card-action-resume" onclick="event.stopPropagation();quickResumeTask('${t.id}',${t.timeout || 15})" title="Resume in existing session">&#8635; Resume</button>`);
    }
    parts.push(`<button class="card-action-btn card-action-retry" onclick="event.stopPropagation();quickRetryTask('${t.id}')" title="Move back to Backlog">&#8617; Retry</button>`);
  } else if (t.status === 'cancelled') {
    parts.push(`<button class="card-action-btn card-action-retry" onclick="event.stopPropagation();quickRetryTask('${t.id}')" title="Move back to Backlog">&#8617; Retry</button>`);
  } else if (t.status === 'done') {
    parts.push(`<button class="card-action-btn card-action-retry" onclick="event.stopPropagation();quickRetryTask('${t.id}')" title="Move back to Backlog">&#8617; Retry</button>`);
  }
  if (!parts.length) return '';
  return `<div class="card-actions">${parts.join('')}</div>`;
}

function updateCard(card, t, rank) {
  const isIdeaAgent = t.kind === 'idea-agent';
  const isArchived = !!t.archived;
  const isTestRun = !!t.is_test_run && t.status === 'in_progress';
  const badgeClass = isArchived ? 'badge-archived' : isTestRun ? 'badge-testing' : `badge-${t.status}`;
  const statusLabel = isArchived ? 'archived' : isTestRun ? 'testing' : (t.status === 'in_progress' ? 'in progress' : t.status === 'committing' ? 'committing' : t.status);
  if (isIdeaAgent) {
    card.classList.add('card-idea-agent');
  } else {
    card.classList.remove('card-idea-agent');
  }
  const showSpinner = t.status === 'in_progress' || t.status === 'committing';
  const showDiff = (t.status === 'waiting' || t.status === 'failed' || t.status === 'done') && t.worktree_paths && Object.keys(t.worktree_paths).length > 0;
  card.style.opacity = isArchived ? '0.55' : '';
  // Failed tasks in the waiting column get a red left border to distinguish them.
  if (t.status === 'failed') {
    card.classList.add('card-failed-waiting');
  } else {
    card.classList.remove('card-failed-waiting');
  }
  // Cancelled tasks in the done column get a purple left border to distinguish them.
  if (t.status === 'cancelled') {
    card.classList.add('card-cancelled-done');
  } else {
    card.classList.remove('card-cancelled-done');
  }
  const displayRank = rank !== undefined ? rank + 1 : t.position + 1;
  const priorityBadge = t.status === 'backlog' ? `<span class="badge badge-priority" title="Priority #${displayRank}">#${displayRank}</span>` : '';
  const isBlocked = t.status === 'backlog' && areDepsBlocked(t);
  const blockedBadge = isBlocked
    ? `<span class="badge badge-blocked" title="Blocked by: ${escapeHtml(getBlockingTaskNames(t))}">\uD83D\uDD12</span>`
    : '';
  const refineJobStatus = t.status === 'backlog' && t.current_refinement && t.current_refinement.status;
  const refinementBadge = refineJobStatus === 'running'
    ? `<span class="badge badge-refining" title="Refinement in progress \u2014 start disabled">refining\u2026</span>`
    : refineJobStatus === 'done'
    ? `<span class="badge badge-refine-review" title="Review refined prompt before starting">review prompt</span>`
    : '';
  const testResultBadge = t.last_test_result === 'pass'
    ? `<span class="badge badge-test-pass" title="Verification passed">\u2713 verified</span>`
    : t.last_test_result === 'fail'
    ? `<span class="badge badge-test-fail" title="Verification failed">\u2717 verify failed</span>`
    : t.last_test_result === 'unknown'
    ? `<span class="badge badge-test-none" title="Tested \u2014 no clear verdict detected">no verdict</span>`
    : t.status === 'waiting'
    ? `<span class="badge badge-test-none" title="Not yet verified">unverified</span>`
    : '';
  const sandbox = t.sandbox || 'default';
  card.innerHTML = `
    <div class="flex items-center justify-between mb-1">
      <div class="flex items-center gap-1.5">
        ${priorityBadge}
        ${blockedBadge}
        <span class="badge ${badgeClass}">${statusLabel}</span>
        ${showSpinner ? '<span class="spinner"></span>' : ''}
        ${refinementBadge}
        ${testResultBadge}
      </div>
      <div class="flex items-center gap-1.5">
        <span class="text-[10px] text-v-muted" title="Sandbox: ${escapeHtml(sandbox)}">${escapeHtml(sandboxDisplayName(sandbox))}</span>
        ${t.mount_worktrees ? '<span class="text-[10px] text-v-muted" title="Sibling worktrees mounted">worktrees</span>' : ''}
        <span class="text-[10px] text-v-muted" title="Timeout">${formatTimeout(t.timeout)}</span>
        <span class="text-[10px] text-v-muted">${timeAgo(t.created_at)}</span>
        ${t.tags && t.tags.length > 0 ? t.tags.map(tag => {
          const cls = BRAINSTORM_CATEGORIES.has(tag) ? 'badge badge-category' : `badge badge-${tag.replace(/[^a-z0-9]/g, '-')}`;
          return `<span class="${cls}" title="Tag: ${escapeHtml(tag)}">${escapeHtml(tag)}</span>`;
        }).join('') : ''}
      </div>
    </div>
    ${t.status === 'backlog' && t.session_id ? `<div class="flex items-center gap-1.5 mb-1" onclick="event.stopPropagation()">
      <input type="checkbox" id="resume-chk-${t.id}" ${!t.fresh_start ? 'checked' : ''} onchange="toggleFreshStart('${t.id}', !this.checked)" style="width:11px;height:11px;cursor:pointer;accent-color:var(--accent);">
      <label for="resume-chk-${t.id}" class="text-[10px] text-v-muted" style="cursor:pointer;">Resume previous session</label>
    </div>` : ''}
    ${isIdeaAgent ? `<div class="card-title">&#129504; ${highlightMatch(t.title || 'Brainstorm', filterQuery)}</div>` : t.title ? `<div class="card-title">${highlightMatch(t.title, filterQuery)}</div>` : ''}
    <div class="text-sm card-prose overflow-hidden" style="max-height:4.5em;">${renderMarkdown(t.prompt)}</div>
    ${t.status === 'failed' && t.result ? `
    <div class="card-error-reason">
      <span class="card-error-label">Error</span><span class="card-error-text">${escapeHtml(t.result.length > 160 ? t.result.slice(0, 160) + '\u2026' : t.result)}</span>
    </div>
    ${t.stop_reason ? `<div style="margin-top:4px;"><span class="badge badge-failed" style="font-size:9px;">${escapeHtml(t.stop_reason)}</span></div>` : ''}
    ` : t.status === 'waiting' && t.result ? `
    <div class="card-output-reason">
      <span class="card-output-label">Output</span><span class="card-output-text">${escapeHtml(t.result.length > 160 ? t.result.slice(0, 160) + '\u2026' : t.result)}</span>
    </div>
    ` : t.result && t.status !== 'in_progress' ? `
    <div class="text-xs text-v-secondary mt-1 card-prose overflow-hidden" style="max-height:3.2em;">${renderMarkdown(t.result)}</div>
    ` : ''}
    ${showDiff ? `<div class="diff-block" data-diff><span style="color:var(--text-muted)">loading diff\u2026</span></div>` : ''}
    ${buildCardActions(t)}
  `;
}
