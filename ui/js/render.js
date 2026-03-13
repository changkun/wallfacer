// Brainstorm category values are loaded from /api/config (backend authoritative
// source) so new categories can be added without frontend code changes.
var BRAINSTORM_CATEGORIES = new Set();
let _taskIndex = new Map();
let _renderableTaskIndex = new Map();

function tagStyle(tag) {
  let sum = 0;
  for (let index = 0; index < tag.length; index++) sum += tag.charCodeAt(index);
  const n = sum % 12;
  return `background:var(--tag-bg-${n});color:var(--tag-text-${n});`;
}

function setBrainstormCategories(values) {
  BRAINSTORM_CATEGORIES = new Set(
    Array.isArray(values)
      ? values.filter(function (value) {
          return typeof value === 'string' && value.trim() !== '';
        }).map(function (value) {
          return value.trim();
        })
      : [],
  );
}

function renderTaskTagBadge(tag) {
  if (!tag) return '';
  const rawTag = String(tag).trim();
  if (!rawTag) return '';

  const lower = rawTag.toLowerCase();
  if (BRAINSTORM_CATEGORIES.has(rawTag)) {
    return `<span class="badge badge-category" title="Tag: ${escapeHtml(rawTag)}">${escapeHtml(rawTag)}</span>`;
  }

  if (lower === 'idea-agent') {
    return `<span class="badge badge-idea-agent" title="Tag: ${escapeHtml(rawTag)}">${escapeHtml(rawTag)}</span>`;
  }

  if (lower.startsWith('priority:')) {
    const priorityValue = rawTag.slice('priority:'.length).trim();
    const text = `priority ${priorityValue}`;
    return `<span class="badge badge-priority" title="Tag: ${escapeHtml(rawTag)}">${escapeHtml(text)}</span>`;
  }

  if (lower.startsWith('impact:')) {
    const impactValue = rawTag.slice('impact:'.length).trim();
    const text = `impact ${impactValue}`;
    return `<span class="badge badge-impact" title="Tag: ${escapeHtml(rawTag)}">${escapeHtml(text)}</span>`;
  }

  return `<span class="badge badge-tag" title="Tag: ${escapeHtml(rawTag)}">${escapeHtml(rawTag)}</span>`;
}

function renderTaskTagBadges(tags) {
  if (!Array.isArray(tags) || tags.length === 0) return '';
  return tags.map(renderTaskTagBadge).join('');
}

// formatRelativeTime returns a short human-readable relative time string for a
// future Date, e.g. "in 3h", "in 45m", "in 2d". Returns '' for past dates.
function formatRelativeTime(date) {
  const diffMs = date - Date.now();
  if (diffMs <= 0) return '';
  const diffSec = Math.floor(diffMs / 1000);
  if (diffSec < 60) return 'in ' + diffSec + 's';
  const diffMin = Math.floor(diffSec / 60);
  if (diffMin < 60) return 'in ' + diffMin + 'm';
  const diffHr = Math.floor(diffMin / 60);
  if (diffHr < 24) return 'in ' + diffHr + 'h';
  const diffDays = Math.floor(diffHr / 24);
  return 'in ' + diffDays + 'd';
}

function getRenderableTasks() {
  if (typeof showArchived !== 'undefined' && showArchived &&
      typeof archivedTasks !== 'undefined' &&
      Array.isArray(archivedTasks) &&
      archivedTasks.length > 0) {
    return tasks.concat(archivedTasks);
  }
  return tasks;
}

function _rebuildTaskIndexes() {
  _taskIndex = new Map();
  for (const task of tasks) _taskIndex.set(task.id, task);
  _renderableTaskIndex = typeof showArchived !== 'undefined' && showArchived &&
    typeof archivedTasks !== 'undefined' &&
    Array.isArray(archivedTasks) &&
    archivedTasks.length > 0
    ? new Map(getRenderableTasks().map(function(task) { return [task.id, task]; }))
    : _taskIndex;
}

function _ensureTaskIndexes() {
  const activeCount = Array.isArray(tasks) ? tasks.length : 0;
  const archivedCount = typeof showArchived !== 'undefined' && showArchived &&
    typeof archivedTasks !== 'undefined' &&
    Array.isArray(archivedTasks)
    ? archivedTasks.length
    : 0;
  const expectedRenderableCount = activeCount + archivedCount;
  if (_taskIndex.size !== activeCount || _renderableTaskIndex.size !== expectedRenderableCount) {
    _rebuildTaskIndexes();
  }
}

function getTaskImpactScore(task) {
  if (!task || typeof task !== 'object') return null;
  if (typeof task.impact_score === 'number' && Number.isFinite(task.impact_score)) {
    return task.impact_score;
  }
  const tags = Array.isArray(task.tags) ? task.tags : [];
  for (const tag of tags) {
    if (typeof tag !== 'string') continue;
    const trimmed = tag.trim();
    if (!trimmed.toLowerCase().startsWith('impact:')) continue;
    const value = Number.parseInt(trimmed.slice('impact:'.length).trim(), 10);
    if (Number.isFinite(value)) return value;
  }
  return null;
}

function sortBacklogTasks(items) {
  const mode = typeof backlogSortMode === 'string' ? backlogSortMode : 'manual';
  if (mode !== 'impact') {
    items.sort((a, b) => a.position - b.position);
    return items;
  }
  items.sort((a, b) => {
    const impactA = getTaskImpactScore(a);
    const impactB = getTaskImpactScore(b);
    const hasImpactA = impactA !== null;
    const hasImpactB = impactB !== null;
    if (hasImpactA && hasImpactB && impactA !== impactB) return impactB - impactA;
    if (hasImpactA !== hasImpactB) return hasImpactA ? -1 : 1;
    return a.position - b.position;
  });
  return items;
}

function updateBacklogSortButton() {
  const button = document.getElementById('backlog-sort-btn');
  if (!button) return;
  const impactSort = (typeof backlogSortMode === 'string' ? backlogSortMode : 'manual') === 'impact';
  button.textContent = impactSort ? 'Sort: Impact' : 'Sort: Manual';
  button.setAttribute('aria-pressed', impactSort ? 'true' : 'false');
  button.classList.toggle('active', impactSort);
}

function setBacklogSortMode(mode) {
  backlogSortMode = mode === 'impact' ? 'impact' : 'manual';
  localStorage.setItem('wallfacer-backlog-sort-mode', backlogSortMode);
  updateBacklogSortButton();
  if (typeof syncBacklogSortableMode === 'function') syncBacklogSortableMode();
}

function toggleBacklogSort() {
  setBacklogSortMode(backlogSortMode === 'impact' ? 'manual' : 'impact');
  render();
}

// --- Dependency badge helpers ---

function areDepsBlocked(t) {
  if (!t.depends_on || t.depends_on.length === 0) return false;
  _ensureTaskIndexes();
  return t.depends_on.some(function(depId) {
    var dep = _renderableTaskIndex.get(depId);
    return !dep || dep.status !== 'done';
  });
}

function getBlockingTaskNames(t) {
  if (!t.depends_on) return '';
  _ensureTaskIndexes();
  return t.depends_on.map(function(id) {
    var dep = _renderableTaskIndex.get(id);
    if (dep && dep.status === 'done') return null;
    if (!dep) return id.slice(0, 8) + '\u2026';
    return dep.title || (dep.prompt.length > 30 ? dep.prompt.slice(0, 30) + '\u2026' : dep.prompt);
  }).filter(function(name) {
    return !!name;
  }).join(', ');
}

function focusFirstCardInColumn(status) {
  const list = document.getElementById('col-' + status);
  if (!list) return;
  const firstCard = list.querySelector('[role="listitem"]');
  if (firstCard && typeof firstCard.focus === 'function') firstCard.focus();
}

function _nextListItem(node) {
  let current = node ? node.nextElementSibling : null;
  while (current) {
    if (current.getAttribute && current.getAttribute('role') === 'listitem') return current;
    current = current.nextElementSibling;
  }
  return null;
}

function _previousListItem(node) {
  let current = node ? node.previousElementSibling : null;
  while (current) {
    if (current.getAttribute && current.getAttribute('role') === 'listitem') return current;
    current = current.previousElementSibling;
  }
  return null;
}

function _firstListItem(list) {
  return list ? list.querySelector('[role="listitem"]') : null;
}

function _lastListItem(list) {
  if (!list) return null;
  const items = list.querySelectorAll('[role="listitem"]');
  return items && items.length ? items[items.length - 1] : null;
}

function _findSiblingColumnCard(card, direction) {
  const region = card && typeof card.closest === 'function' ? card.closest('[role="region"]') : null;
  let sibling = region ? (direction < 0 ? region.previousElementSibling : region.nextElementSibling) : null;
  while (sibling) {
    if (sibling.getAttribute && sibling.getAttribute('role') === 'region') {
      const list = sibling.querySelector('[role="list"]');
      const target = direction < 0 ? _lastListItem(list) : _firstListItem(list);
      if (target) return target;
    }
    sibling = direction < 0 ? sibling.previousElementSibling : sibling.nextElementSibling;
  }
  return null;
}

function _cardDescriptionId(taskId, kind) {
  return 'card-' + taskId + '-' + kind;
}

function _bindCardKeyboardNavigation(card, t) {
  if (card._keydownHandler) {
    card.removeEventListener('keydown', card._keydownHandler);
  }
  card._keydownHandler = function(e) {
    let target = null;
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      openModal(t.id);
      return;
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      target = _previousListItem(card) || _lastListItem(card.parentElement);
      if (target) target.focus();
      return;
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      target = _nextListItem(card) || _firstListItem(card.parentElement);
      if (target) target.focus();
      return;
    }
    if (e.key === 'ArrowLeft') {
      e.preventDefault();
      target = _findSiblingColumnCard(card, -1);
      if (target) target.focus();
      return;
    }
    if (e.key === 'ArrowRight') {
      e.preventDefault();
      target = _findSiblingColumnCard(card, 1);
      if (target) target.focus();
      return;
    }
    if (e.key === 's' && t.status === 'backlog') {
      e.preventDefault();
      updateTaskStatus(t.id, 'in_progress');
      return;
    }
    if (e.key === 'd' && t.status === 'waiting') {
      e.preventDefault();
      quickDoneTask(t.id);
      return;
    }
    if (e.key === 'Escape') {
      e.preventDefault();
      card.blur();
      const board = document.getElementById('board');
      if (board && typeof board.focus === 'function') board.focus();
    }
  };
  card.addEventListener('keydown', card._keydownHandler);
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

const BEHIND_TTL_MS = 5 * 60 * 1000; // 5 minutes — how long a behind-count stays fresh without an explicit invalidation
const diffCache = new Map(); // taskId -> {diff: string, behindCounts, updatedAt, behindFetchedAt} | 'loading'
const cardOversightCache = new Map(); // taskId -> {phase_count, phases}

function isTestCard(task) {
  return !!task.last_test_result && task.test_run_start_turn > 0;
}

function hasExecutionTrail(t) {
  return (t.turns || 0) > 0 || !!t.result || !!t.stop_reason;
}

// Invalidate cached diff/behind-count state so that the next render re-fetches
// data. Preserve the cached diff body so cards can continue rendering the last
// known summary while forcing a behind-count refresh on the next fetch.
function invalidateDiffBehindCounts(taskId) {
  if (taskId) {
    const cached = diffCache.get(taskId);
    if (cached && cached !== 'loading') cached.behindFetchedAt = 0;
  } else {
    diffCache.forEach(function(cached) {
      if (cached && cached !== 'loading') cached.behindFetchedAt = 0;
    });
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
  // Cache is valid if the task hasn't changed AND behind-counts are still fresh.
  // invalidateDiffBehindCounts() evicts stale entries explicitly for changed tasks,
  // and BEHIND_TTL_MS provides a fallback refresh window for slowly advancing
  // default branches.
  if (cached && cached.updatedAt === updatedAt &&
      cached.behindFetchedAt &&
      (Date.now() - cached.behindFetchedAt) < BEHIND_TTL_MS) {
    const diffEl = card.querySelector('[data-diff]');
    if (diffEl) applyDiffToCard(diffEl, cached.diff, cached.behindCounts, taskId);
    return;
  }
  diffCache.set(taskId, 'loading');
  try {
    const diffPath = typeof task === 'function'
      ? task(taskId).diff()
      : '/api/tasks/' + encodeURIComponent(taskId) + '/diff';
    const data = await api(diffPath);
    const behindCounts = data.behind_counts || {};
    diffCache.set(taskId, { diff: data.diff, behindCounts, updatedAt, behindFetchedAt: Date.now() });
    const latestEl = card.querySelector('[data-diff]');
    if (latestEl) applyDiffToCard(latestEl, data.diff, behindCounts, taskId);
  } catch {
    diffCache.delete(taskId);
  }
}

function applyDiffToCard(el, diff, behindCounts, taskId) {
  const task = typeof tasks !== 'undefined' ? tasks.find(t => t.id === taskId) : null;
  const entries = Object.entries(behindCounts || {});
  const totalBehind = entries.reduce((s, [, n]) => s + n, 0);
  let warning = '';
  if (totalBehind > 0 && !(task && isTestCard(task))) {
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
  _rebuildTaskIndexes();
  // Sync ideation spinner from live task list (no polling needed).
  if (typeof updateIdeationFromTasks === 'function') updateIdeationFromTasks(tasks);
  updateBacklogSortButton();

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
  if (showArchived && Array.isArray(archivedTasks) && archivedTasks.length > 0) {
    const seenDone = new Set(columns.done.map(function(t) { return t.id; }));
    for (const archivedTask of archivedTasks) {
      if ((archivedTask.status !== 'done' && archivedTask.status !== 'cancelled') || seenDone.has(archivedTask.id)) {
        continue;
      }
      columns.done.push(archivedTask);
      seenDone.add(archivedTask.id);
    }
  }

  const _colTitles = { backlog: 'Backlog', in_progress: 'In Progress', waiting: 'Waiting', done: 'Done' };
  for (const [status, items] of Object.entries(columns)) {
    const el = document.getElementById(`col-${status}`);
    if (!el) continue;
    if (!el.hasAttribute('role')) el.setAttribute('role', 'list');
    if (!el.hasAttribute('aria-live')) el.setAttribute('aria-live', 'polite');
    if (!el.hasAttribute('aria-relevant')) el.setAttribute('aria-relevant', 'additions removals');
    if (!el.hasAttribute('aria-label')) el.setAttribute('aria-label', `${_colTitles[status] || status} tasks`);

    // Backlog: sort by position ascending (priority order).
    // Other columns: sort by last updated descending.
    if (status === 'backlog') {
      sortBacklogTasks(items);
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
      // Load diff for any task that has worktrees
      if (t.worktree_paths && Object.keys(t.worktree_paths).length > 0) {
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
  if (getOpenModalTaskId()) {
    const openTask = getRenderableTasks().find(t => t.id === getOpenModalTaskId());
    if (openTask && openTask.status === 'backlog') {
      updateRefineUI(openTask);
      renderRefineHistory(openTask);
    }
  }

  if (window.depGraphEnabled && typeof renderDependencyGraph === 'function') renderDependencyGraph(getRenderableTasks());
  else if (typeof hideDependencyGraph === 'function') hideDependencyGraph();
}

// --- Board render scheduler ---
// Coalesces rapid back-to-back render() calls (e.g. SSE bursts) into a single
// paint per animation frame so the main thread stays responsive.
let _renderPending = false;
function scheduleRender() {
  if (_renderPending) return;
  _renderPending = true;
  requestAnimationFrame(function() {
    _renderPending = false;
    render();
  });
}

// --- Markdown cache ---
// marked.parse() is expensive; cache results keyed by source text so unchanged
// card content is not re-parsed on every render cycle.
const _mdCache = new Map();
function _cachedMarkdown(text) {
  if (!text) return '';
  if (_mdCache.has(text)) return _mdCache.get(text);
  const html = renderMarkdown(text);
  // Evict the oldest entry once the cache grows large (>1 000 unique strings)
  // to avoid unbounded memory growth in very long-running sessions.
  if (_mdCache.size >= 1000) _mdCache.delete(_mdCache.keys().next().value);
  _mdCache.set(text, html);
  return html;
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

// _cardFingerprint computes a lightweight fingerprint string for the card-relevant
// fields of a task so that updateCard can skip the expensive innerHTML rebuild
// when nothing visible has changed.
function _cardFingerprint(t, rank) {
  const displayRank = rank !== undefined ? rank + 1 : t.position + 1;
  // Include the status of each dependency so the blocked badge updates
  // immediately when a dependency moves to done/failed without waiting for
  // the dependent task itself to change.
  const depStatuses = (t.depends_on || []).map(depId => {
    const dep = _taskIndex.get(depId);
    return dep ? dep.status : '';
  }).join(',');
  return [
    t.status, t.kind, !!t.archived, !!t.is_test_run, t.title || '',
    t.prompt, t.execution_prompt || '', t.result || '', t.updated_at, t.session_id || '',
    !!t.fresh_start, t.timeout, t.stop_reason || '', t.last_test_result || '',
    t.sandbox || '', JSON.stringify(t.sandbox_by_activity || {}),
    !!t.mount_worktrees, JSON.stringify(t.tags || []),
    JSON.stringify(t.depends_on || []), depStatuses,
    t.current_refinement ? t.current_refinement.status : '',
    JSON.stringify(t.worktree_paths || {}), displayRank,
    filterQuery,
    t.max_cost_usd || 0,
    (t.usage && t.usage.cost_usd) || 0,
    t.max_input_tokens || 0,
    t.scheduled_at || '',
    t.failure_category || '',
  ].join('\x00');
}

function cardDisplayPrompt(t) {
  if (typeof taskDisplayPrompt === 'function') return taskDisplayPrompt(t);
  if (t && t.kind === 'idea-agent' && t.execution_prompt) return t.execution_prompt;
  return t ? t.prompt : '';
}

function updateCard(card, t, rank) {
  // Skip the expensive innerHTML rebuild if no visible data has changed.
  const fp = _cardFingerprint(t, rank);
  if (card.dataset.fp === fp) return;
  card.dataset.fp = fp;

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
  const showDiff = !!(t.worktree_paths && Object.keys(t.worktree_paths).length > 0);
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
  const scheduledBadge = (t.status === 'backlog' && t.scheduled_at && new Date(t.scheduled_at) > new Date())
    ? `<span class="badge badge-scheduled" title="Scheduled: ${escapeHtml(new Date(t.scheduled_at).toLocaleString())}">\u23F0 ${escapeHtml(formatRelativeTime(new Date(t.scheduled_at)))}</span>`
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
  const _failureCategoryLabels = {
    'timeout': 'Timeout',
    'budget_exceeded': 'Budget',
    'container_crash': 'Crash',
    'agent_error': 'Agent Error',
    'worktree_setup': 'Worktree',
    'sync_error': 'Sync',
    'unknown': '',
  };
  const _fcLabel = t.status === 'failed' && t.failure_category ? _failureCategoryLabels[t.failure_category] : '';
  const failureCategoryBadge = _fcLabel
    ? `<span class="badge badge-failure-category" title="Failure reason: ${escapeHtml(t.failure_category)}" style="font-family:monospace;font-size:9px;">${escapeHtml(_fcLabel)}</span>`
    : '';
  const implSandbox = (t.sandbox_by_activity && t.sandbox_by_activity.implementation) || t.sandbox || 'default';
  const cardTitle = typeof getTaskAccessibleTitle === 'function'
    ? getTaskAccessibleTitle(t)
    : (t.title || t.prompt || t.id);
  const cardStatusLabel = typeof formatTaskStatusLabel === 'function'
    ? formatTaskStatusLabel(statusLabel)
    : String(statusLabel || '').replace(/_/g, ' ');
  card.innerHTML = `
    <div class="flex items-center justify-between mb-1">
      <div class="flex items-center gap-1.5">
        ${priorityBadge}
        ${blockedBadge}
        ${scheduledBadge}
        <span class="badge ${badgeClass}">${statusLabel}</span>
        ${showSpinner ? '<span class="spinner"></span>' : ''}
        ${refinementBadge}
        ${testResultBadge}
        ${failureCategoryBadge}
      </div>
      <div class="flex items-center gap-1.5 card-meta-right">
        ${t.model_override ? `<span class="text-[10px] text-v-muted" title="Model override: ${escapeHtml(t.model_override)}">&#9881; ${escapeHtml(t.model_override.length > 20 ? t.model_override.slice(0, 20) + '\u2026' : t.model_override)}</span>` : ''}
        <span class="text-[10px] text-v-muted" title="Implementation sandbox: ${escapeHtml(implSandbox)}">${escapeHtml(sandboxDisplayName(implSandbox))}</span>
        ${t.mount_worktrees ? '<span class="text-[10px] text-v-muted" title="Sibling worktrees mounted">worktrees</span>' : ''}
        <span class="text-[10px] text-v-muted" title="Timeout">${formatTimeout(t.timeout)}</span>
        <span class="text-[10px] text-v-muted">${timeAgo(t.created_at)}</span>
      </div>
    </div>
    ${t.status === 'backlog' && t.session_id ? `<div class="flex items-center gap-1.5 mb-1" onclick="event.stopPropagation()">
      <input type="checkbox" id="resume-chk-${t.id}" ${!t.fresh_start ? 'checked' : ''} onchange="toggleFreshStart('${t.id}', !this.checked)" style="width:11px;height:11px;cursor:pointer;accent-color:var(--accent);">
      <label for="resume-chk-${t.id}" class="text-[10px] text-v-muted" style="cursor:pointer;">Resume previous session</label>
    </div>` : ''}
    ${isIdeaAgent ? `<div class="card-title">&#129504; ${highlightMatch(t.title || 'Brainstorm', filterQuery)}</div>` : t.title ? `<div class="card-title">${highlightMatch(t.title, filterQuery)}</div>` : ''}
    ${t.tags && t.tags.length > 0 ? (() => {
      const VISIBLE = 3;
      const overflow = t.tags.length - VISIBLE;
      const chips = t.tags.map((tag, i) => {
        const extra = i >= VISIBLE ? ' tag-chip-extra' : '';
        return `<span class="tag-chip${extra}" data-tag="${escapeHtml(tag)}" style="${tagStyle(tag)}" title="${escapeHtml(tag)}">${escapeHtml(tag)}</span>`;
      }).join('');
      const overflowChip = overflow > 0
        ? `<span class="tag-chip tag-chip-overflow" onclick="event.stopPropagation();this.closest('.tag-chip-row').classList.toggle('expanded');" title="Show all tags">+${overflow}</span>`
        : '';
      return `<div class="tag-chip-row">${chips}${overflowChip}</div>`;
    })() : ''}
    <div class="text-sm card-prose overflow-hidden" style="max-height:4.5em;">${_cachedMarkdown(cardDisplayPrompt(t))}</div>
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
    <div class="text-xs text-v-secondary mt-1 card-prose overflow-hidden" style="max-height:3.2em;">${_cachedMarkdown(t.result)}</div>
    ` : ''}
    ${showDiff ? `<div class="diff-block" data-diff><span style="color:var(--text-muted)">loading diff\u2026</span></div>` : ''}
    ${(t.max_cost_usd > 0 && (t.status === 'in_progress' || t.status === 'waiting')) ? (() => {
      const spent = (t.usage && t.usage.cost_usd) || 0;
      const pct = Math.min(100, (spent / t.max_cost_usd) * 100);
      const color = pct >= 90 ? 'var(--red,#ef4444)' : pct >= 70 ? 'var(--yellow,#f59e0b)' : 'var(--green,#22c55e)';
      return `<div style="margin-top:4px;height:3px;border-radius:2px;background:var(--border);overflow:hidden;" title="Cost: $${spent.toFixed(4)} of $${t.max_cost_usd.toFixed(2)} budget"><div style="height:100%;width:${pct}%;background:${color};transition:width 0.3s;"></div></div>`;
    })() : ''}
    ${buildCardActions(t)}
  `;

  // Fork ancestry badge — rendered after innerHTML is set.
  if (t.forked_from) {
    const badge = document.createElement('div');
    badge.className = 'text-[10px] text-v-muted mt-1';
    const shortParent = t.forked_from.substring(0, 8);
    badge.textContent = '\u2442 ' + shortParent;  // ⑂ fork symbol
    badge.title = 'Forked from task ' + shortParent;
    badge.style.cursor = 'pointer';
    badge.addEventListener('click', (e) => {
      e.stopPropagation();
      const allTasks = [...tasks, ...(Array.isArray(archivedTasks) ? archivedTasks : [])];
      const parent = allTasks.find(p => p.id === t.forked_from);
      if (parent) openModal(parent.id);
    });
    card.appendChild(badge);
  }

  card.tabIndex = 0;
  if (typeof card.setAttribute === 'function') {
    card.setAttribute('role', 'listitem');
    card.setAttribute('aria-label', `${cardTitle} — ${cardStatusLabel}`);
  }

  const promptEl = card.querySelector('.card-prose');
  if (promptEl && !promptEl.id) promptEl.id = _cardDescriptionId(t.id, 'prompt');
  const waitingResultEl = card.querySelector('.card-output-text');
  if (waitingResultEl && !waitingResultEl.id) waitingResultEl.id = _cardDescriptionId(t.id, 'result');
  const failedResultEl = card.querySelector('.card-error-text');
  if (failedResultEl && !failedResultEl.id) failedResultEl.id = _cardDescriptionId(t.id, 'error');
  const describedByEl = failedResultEl || waitingResultEl || promptEl;
  if (typeof card.setAttribute === 'function' && describedByEl && describedByEl.id) {
    card.setAttribute('aria-describedby', describedByEl.id);
  } else if (typeof card.removeAttribute === 'function') {
    card.removeAttribute('aria-describedby');
  }

  _bindCardKeyboardNavigation(card, t);

}

if (typeof module !== 'undefined') {
  module.exports = {
    areDepsBlocked,
    getBlockingTaskNames,
    isTestCard,
    invalidateDiffBehindCounts,
    BEHIND_TTL_MS,
    diffCache,
    cardOversightCache,
    fetchDiff,
  };
}
