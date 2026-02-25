// --- Board rendering ---

const diffCache = new Map(); // taskId -> {diff: string, updatedAt: string} | 'loading'

function renderDiffInto(el, diff) {
  if (!diff) {
    el.innerHTML = '<span style="color:var(--text-muted)">no changes</span>';
    return;
  }
  const lines = diff.split('\n');
  el.innerHTML = lines.map(line => {
    const escaped = escapeHtml(line);
    if (line.startsWith('+') && !line.startsWith('+++')) {
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
  if (cached && cached.updatedAt === updatedAt) {
    const diffEl = card.querySelector('[data-diff]');
    if (diffEl) applyDiffToCard(diffEl, cached.diff, cached.behindCounts, taskId);
    return;
  }
  diffCache.set(taskId, 'loading');
  try {
    const data = await api(`/api/tasks/${taskId}/diff`);
    const behindCounts = data.behind_counts || {};
    diffCache.set(taskId, { diff: data.diff, behindCounts, updatedAt });
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
    const countEl = document.getElementById(`count-${status}`);
    if (countEl) countEl.textContent = items.length;

    const existing = new Map();
    for (const child of el.children) {
      existing.set(child.dataset.id, child);
    }

    // Sort by last updated descending (most recently updated first)
    items.sort((a, b) => new Date(b.updated_at) - new Date(a.updated_at));

    const newIds = new Set(items.map(t => t.id));

    // Remove cards that are no longer in this column
    for (const [id, child] of existing) {
      if (!newIds.has(id)) child.remove();
    }

    // Add or update cards, maintaining sorted order in the DOM
    for (let i = 0; i < items.length; i++) {
      const t = items[i];
      let card = existing.get(t.id);
      if (!card) {
        card = createCard(t);
      } else {
        updateCard(card, t);
      }
      if (el.children[i] !== card) {
        el.insertBefore(card, el.children[i] || null);
      }
      // Load diff for waiting/failed tasks that have worktrees
      if ((t.status === 'waiting' || t.status === 'failed') && t.worktree_paths && Object.keys(t.worktree_paths).length > 0) {
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
}

function createCard(t) {
  const card = document.createElement('div');
  card.className = 'card';
  card.dataset.id = t.id;
  card.onclick = () => openModal(t.id);
  updateCard(card, t);
  return card;
}

function buildCardActions(t) {
  if (t.archived) return '';
  const parts = [];
  if (t.status === 'backlog') {
    parts.push(`<button class="card-action-btn card-action-start" onclick="event.stopPropagation();updateTaskStatus('${t.id}','in_progress')" title="Move to In Progress">&#9654; Start</button>`);
  } else if (t.status === 'waiting') {
    parts.push(`<button class="card-action-btn card-action-done" onclick="event.stopPropagation();quickDoneTask('${t.id}')" title="Mark done and commit">&#10003; Mark done</button>`);
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

function updateCard(card, t) {
  const isArchived = !!t.archived;
  const badgeClass = isArchived ? 'badge-archived' : `badge-${t.status}`;
  const statusLabel = isArchived ? 'archived' : (t.status === 'in_progress' ? 'in progress' : t.status === 'committing' ? 'committing' : t.status);
  const showSpinner = t.status === 'in_progress' || t.status === 'committing';
  const showDiff = (t.status === 'waiting' || t.status === 'failed') && t.worktree_paths && Object.keys(t.worktree_paths).length > 0;
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
  card.innerHTML = `
    <div class="flex items-center justify-between mb-1">
      <div class="flex items-center gap-1.5">
        <span class="badge ${badgeClass}">${statusLabel}</span>
        ${showSpinner ? '<span class="spinner"></span>' : ''}
      </div>
      <div class="flex items-center gap-1.5">
        ${t.mount_worktrees ? '<span class="text-[10px] text-v-muted" title="Sibling worktrees mounted">worktrees</span>' : ''}
        <span class="text-[10px] text-v-muted" title="Timeout">${formatTimeout(t.timeout)}</span>
        <span class="text-[10px] text-v-muted">${timeAgo(t.created_at)}</span>
      </div>
    </div>
    ${t.status === 'backlog' && t.session_id ? `<div class="flex items-center gap-1.5 mb-1" onclick="event.stopPropagation()">
      <input type="checkbox" id="resume-chk-${t.id}" ${!t.fresh_start ? 'checked' : ''} onchange="toggleFreshStart('${t.id}', !this.checked)" style="width:11px;height:11px;cursor:pointer;accent-color:var(--accent);">
      <label for="resume-chk-${t.id}" class="text-[10px] text-v-muted" style="cursor:pointer;">Resume previous session</label>
    </div>` : ''}
    ${t.title ? `<div class="card-title">${escapeHtml(t.title)}</div>` : ''}
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
