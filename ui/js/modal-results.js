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
