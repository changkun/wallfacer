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
  if (task.status === 'in_progress') {
    logsSection.classList.remove('hidden');
    startLogStream(id);
  } else {
    logsSection.classList.add('hidden');
  }

  const feedbackSection = document.getElementById('modal-feedback-section');
  feedbackSection.classList.toggle('hidden', task.status !== 'waiting');

  // Resume section (failed with session_id only)
  const resumeSection = document.getElementById('modal-resume-section');
  if (task.status === 'failed' && task.session_id) {
    resumeSection.classList.remove('hidden');
  } else {
    resumeSection.classList.add('hidden');
  }

  // Retry section (done/failed only)
  const retrySection = document.getElementById('modal-retry-section');
  if (task.status === 'done' || task.status === 'failed') {
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
  document.getElementById('modal-logs').textContent = '';
  currentTaskId = null;
  document.getElementById('modal').classList.add('hidden');
  document.getElementById('modal').classList.remove('flex');
}

function startLogStream(id) {
  if (logsAbort) logsAbort.abort();
  logsAbort = new AbortController();
  const logsEl = document.getElementById('modal-logs');
  logsEl.textContent = '';
  const decoder = new TextDecoder();
  const ansiRegex = /\x1b\[[0-9;]*[a-zA-Z]/g;

  fetch(`/api/tasks/${id}/logs`, { signal: logsAbort.signal })
    .then(res => {
      if (!res.ok || !res.body) return;
      const reader = res.body.getReader();
      function read() {
        reader.read().then(({ done, value }) => {
          if (done) return;
          const text = decoder.decode(value, { stream: true });
          const cleaned = text.replace(ansiRegex, '');
          logsEl.textContent += cleaned;
          logsEl.scrollTop = logsEl.scrollHeight;
          read();
        }).catch(() => {});
      }
      read();
    })
    .catch(() => {});
}
