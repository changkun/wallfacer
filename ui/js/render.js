// --- Board rendering ---

function render() {
  const columns = { backlog: [], in_progress: [], waiting: [], committing: [], done: [], failed: [] };
  for (const t of tasks) {
    const col = columns[t.status];
    if (col) col.push(t);
  }

  // Committing tasks show in the Waiting column with a spinner
  columns.waiting = columns.waiting.concat(columns.committing);
  delete columns.committing;

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

function updateCard(card, t) {
  const isArchived = !!t.archived;
  const badgeClass = isArchived ? 'badge-archived' : `badge-${t.status}`;
  const statusLabel = isArchived ? 'archived' : (t.status === 'in_progress' ? 'in progress' : t.status === 'committing' ? 'committing' : t.status);
  const showSpinner = t.status === 'in_progress' || t.status === 'committing';
  card.style.opacity = isArchived ? '0.55' : '';
  card.innerHTML = `
    <div class="flex items-center justify-between mb-1">
      <div class="flex items-center gap-1.5">
        <span class="badge ${badgeClass}">${statusLabel}</span>
        ${showSpinner ? '<span class="spinner"></span>' : ''}
      </div>
      <div class="flex items-center gap-1.5">
        <span class="text-[10px] text-v-muted" title="Timeout">${formatTimeout(t.timeout)}</span>
        <span class="text-[10px] text-v-muted">${timeAgo(t.created_at)}</span>
      </div>
    </div>
    ${t.status === 'backlog' && t.session_id ? `<div class="flex items-center gap-1.5 mb-1" onclick="event.stopPropagation()">
      <input type="checkbox" id="resume-chk-${t.id}" ${!t.fresh_start ? 'checked' : ''} onchange="toggleFreshStart('${t.id}', !this.checked)" style="width:11px;height:11px;cursor:pointer;accent-color:var(--accent);">
      <label for="resume-chk-${t.id}" class="text-[10px] text-v-muted" style="cursor:pointer;">Resume previous session</label>
    </div>` : ''}
    <div class="text-sm card-prose overflow-hidden" style="max-height:4.5em;">${renderMarkdown(t.prompt)}</div>
    ${t.result ? `
    <div class="text-xs text-v-secondary mt-1 card-prose overflow-hidden" style="max-height:3.2em;">${renderMarkdown(t.result)}</div>
    ` : ''}
  `;
}
