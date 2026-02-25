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

function renderResultsFromEvents(results) {
  const section = document.getElementById('modal-result-section');
  const listEl = document.getElementById('modal-results-list');
  if (!results || results.length === 0) {
    section.classList.add('hidden');
    return;
  }

  const heading = section.querySelector('.section-title');
  if (heading) heading.textContent = results.length > 1 ? 'Results' : 'Result';

  const totalTurns = results.length;
  // Display newest turn first so the most recent result is immediately visible.
  listEl.innerHTML = [...results].reverse().map(function(result, i) {
    const isNewest = i === 0;
    const originalIndex = totalTurns - 1 - i; // chronological index (0-based)
    const isPlan = detectResultType(result) === 'plan';
    const entryId = 'result-entry-' + originalIndex;

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

  section.classList.remove('hidden');
}

// --- Diff helpers ---

function parseDiffByFile(diff) {
  const files = [];
  const blocks = diff.split(/(?=^diff --git )/m);
  for (const block of blocks) {
    if (!block.trim()) continue;
    const lines = block.split('\n');
    const match = lines[0].match(/^diff --git a\/.+ b\/(.+)$/);
    const filename = match ? match[1] : lines[0];
    let adds = 0, dels = 0;
    for (const line of lines.slice(1)) {
      if (line.startsWith('+') && !line.startsWith('+++')) adds++;
      if (line.startsWith('-') && !line.startsWith('---')) dels++;
    }
    files.push({ filename, content: block, adds, dels });
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
  container.innerHTML = files.map(f => {
    const statsHtml = [
      f.adds > 0 ? `<span class="diff-add">+${f.adds}</span>` : '',
      f.dels > 0 ? `<span class="diff-del">&minus;${f.dels}</span>` : '',
    ].filter(Boolean).join(' ');
    const diffHtml = f.content.split('\n').map(renderDiffLine).join('\n');
    return `<details class="diff-file">
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

  const editSection = document.getElementById('modal-edit-section');
  if (task.status === 'backlog') {
    document.getElementById('modal-prompt-rendered').classList.add('hidden');
    document.getElementById('modal-prompt').classList.add('hidden');
    document.getElementById('modal-prompt-actions').classList.add('hidden');
    editSection.classList.remove('hidden');
    document.getElementById('modal-edit-prompt').value = task.prompt;
    document.getElementById('modal-edit-timeout').value = String(task.timeout || 5);
    const resumeRow = document.getElementById('modal-edit-resume-row');
    if (task.session_id) {
      resumeRow.classList.remove('hidden');
      document.getElementById('modal-edit-resume').checked = !task.fresh_start;
    } else {
      resumeRow.classList.add('hidden');
    }
    document.getElementById('modal-edit-mount-worktrees').checked = !!task.mount_worktrees;
  } else {
    const promptRaw = document.getElementById('modal-prompt');
    const promptRendered = document.getElementById('modal-prompt-rendered');
    promptRaw.textContent = task.prompt;
    promptRendered.innerHTML = renderMarkdown(task.prompt);
    promptRendered.classList.remove('hidden');
    promptRaw.classList.add('hidden');
    document.getElementById('modal-prompt-actions').classList.remove('hidden');
    document.getElementById('toggle-prompt-btn').textContent = 'Raw';
    editSection.classList.add('hidden');
  }

  // Show task.result as a single-entry fallback; replaced below once events load
  if (task.result) {
    renderResultsFromEvents([task.result]);
  } else {
    document.getElementById('modal-result-section').classList.add('hidden');
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
  } else {
    usageSection.classList.add('hidden');
  }

  const logsSection = document.getElementById('modal-logs-section');
  if (task.status !== 'backlog') {
    logsSection.classList.remove('hidden');
    startLogStream(id);
  } else {
    logsSection.classList.add('hidden');
  }

  const feedbackSection = document.getElementById('modal-feedback-section');
  feedbackSection.classList.toggle('hidden', task.status !== 'waiting');

  // Diff section (waiting/failed tasks with worktrees) — shown in right panel
  const modalCard = document.querySelector('#modal .modal-card');
  const modalRight = document.getElementById('modal-right');
  const hasWorktrees = task.worktree_paths && Object.keys(task.worktree_paths).length > 0;
  const modalBody = document.getElementById('modal-body');
  if ((task.status === 'waiting' || task.status === 'failed') && hasWorktrees) {
    modalCard.classList.add('modal-wide');
    modalRight.classList.remove('hidden');
    modalBody.style.display = 'flex';
    modalBody.style.gap = '0';
    const filesEl = document.getElementById('modal-diff-files');
    const behindEl = document.getElementById('modal-diff-behind');
    filesEl.innerHTML = '<span class="text-xs text-v-muted">Loading diff\u2026</span>';
    if (behindEl) behindEl.classList.add('hidden');
    api(`/api/tasks/${task.id}/diff`).then(data => {
      const el = document.getElementById('modal-diff-files');
      if (el) renderDiffFiles(el, data.diff);
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

    // Replace single-result fallback with all turn results from output events
    const outputResults = events
      .filter(e => e.event_type === 'output' && e.data && e.data.result)
      .map(e => e.data.result);
    if (outputResults.length > 0) {
      renderResultsFromEvents(outputResults);
    }

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
  rawLogBuffer = '';
  document.getElementById('modal-logs').innerHTML = '';
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

function renderLogs() {
  const logsEl = document.getElementById('modal-logs');
  const btn = document.getElementById('toggle-logs-btn');
  if (logsPrettyMode) {
    logsEl.innerHTML = renderPrettyLogs(rawLogBuffer);
    if (btn) btn.textContent = 'Raw';
  } else {
    logsEl.textContent = rawLogBuffer.replace(/\x1b\[[0-9;]*[a-zA-Z]/g, '');
    if (btn) btn.textContent = 'Pretty';
  }
  logsEl.scrollTop = logsEl.scrollHeight;
}

function toggleLogsMode() {
  logsPrettyMode = !logsPrettyMode;
  renderLogs();
}

function startLogStream(id) {
  logsPrettyMode = true;
  _fetchLogs(id);
}

function _fetchLogs(id, retryDelay) {
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
