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

  const resultSection = document.getElementById('modal-result-section');
  if (task.result) {
    const resultRaw = document.getElementById('modal-result');
    const resultRendered = document.getElementById('modal-result-rendered');
    resultRaw.textContent = task.result;
    resultRendered.innerHTML = renderMarkdown(task.result);
    resultRendered.classList.remove('hidden');
    resultRaw.classList.add('hidden');
    document.getElementById('toggle-result-btn').textContent = 'Raw';
    resultSection.classList.remove('hidden');
  } else {
    resultSection.classList.add('hidden');
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
  const modalCard = document.querySelector('.modal-card');
  const modalRight = document.getElementById('modal-right');
  const hasWorktrees = task.worktree_paths && Object.keys(task.worktree_paths).length > 0;
  if ((task.status === 'waiting' || task.status === 'failed') && hasWorktrees) {
    modalCard.classList.add('modal-wide');
    modalRight.classList.remove('hidden');
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
  }

  // Resume section (failed with session_id only)
  const resumeSection = document.getElementById('modal-resume-section');
  if (task.status === 'failed' && task.session_id) {
    resumeSection.classList.remove('hidden');
  } else {
    resumeSection.classList.add('hidden');
  }

  // Cancel section (backlog / in_progress / waiting / failed)
  const cancelSection = document.getElementById('modal-cancel-section');
  const cancellable = ['backlog', 'in_progress', 'waiting', 'failed'];
  cancelSection.classList.toggle('hidden', !cancellable.includes(task.status));

  // Retry section (done / failed / cancelled)
  const retrySection = document.getElementById('modal-retry-section');
  if (task.status === 'done' || task.status === 'failed' || task.status === 'cancelled') {
    retrySection.classList.remove('hidden');
    document.getElementById('modal-retry-prompt').value = task.prompt;
  } else {
    retrySection.classList.add('hidden');
  }

  // Archive/Unarchive section (done tasks only)
  const archiveSection = document.getElementById('modal-archive-section');
  const unarchiveSection = document.getElementById('modal-unarchive-section');
  if (task.status === 'done' && !task.archived) {
    archiveSection.classList.remove('hidden');
    unarchiveSection.classList.add('hidden');
  } else if (task.status === 'done' && task.archived) {
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
      } else if (e.event_type === 'error') {
        detail = escapeHtml(data.error);
      }
      const typeClasses = {
        state_change: 'ev-state',
        output: 'ev-output',
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
  document.querySelector('.modal-card').classList.remove('modal-wide');
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

function _fetchLogs(id) {
  if (logsAbort) logsAbort.abort();
  logsAbort = new AbortController();
  rawLogBuffer = '';
  const logsEl = document.getElementById('modal-logs');
  logsEl.innerHTML = '';
  const decoder = new TextDecoder();
  const url = `/api/tasks/${id}/logs?raw=true`;

  fetch(url, { signal: logsAbort.signal })
    .then(res => {
      if (!res.ok || !res.body) return;
      const reader = res.body.getReader();
      function read() {
        reader.read().then(({ done, value }) => {
          if (done) return;
          rawLogBuffer += decoder.decode(value, { stream: true });
          renderLogs();
          read();
        }).catch(() => {});
      }
      read();
    })
    .catch(() => {});
}
