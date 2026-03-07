// --- Task refinement chat interface ---

let refineTaskId = null;
let refineConversation = []; // [{role, content}] — full conversation shown in chat
let refineLoading = false;

function openRefineModal(taskId) {
  const task = tasks.find(t => t.id === taskId);
  if (!task) return;

  refineTaskId = taskId;
  refineConversation = [];
  refineLoading = false;

  // Populate header.
  document.getElementById('refine-task-id').textContent = 'ID: ' + taskId;
  document.getElementById('refine-current-prompt').textContent = task.prompt;
  document.getElementById('refine-proposed-prompt').value = task.prompt;
  document.getElementById('refine-proposal-hint').classList.add('hidden');

  // Left panel: render prompt as markdown.
  document.getElementById('refine-inline-prompt-rendered').innerHTML = renderMarkdown(task.prompt);

  // Settings: pre-populate from task values.
  const modelEl = document.getElementById('refine-inline-model');
  if (modelEl) modelEl.value = task.model || '';
  const timeoutEl = document.getElementById('refine-inline-timeout');
  if (timeoutEl) timeoutEl.value = task.timeout || 60;
  const worktreesEl = document.getElementById('refine-inline-mount-worktrees');
  if (worktreesEl) worktreesEl.checked = !!task.mount_worktrees;

  // Clear chat.
  document.getElementById('refine-chat-messages').innerHTML = '';
  document.getElementById('refine-chat-input').value = '';

  // Populate history from task's refine_sessions.
  renderRefineHistory(task);

  // Show inline panel, hide board.
  document.getElementById('board').classList.add('hidden');
  document.getElementById('refine-inline').classList.remove('hidden');

  // Kick off the opening question from the AI.
  sendRefineRequest('');
}

function closeRefineModal() {
  refineTaskId = null;
  refineConversation = [];
  refineLoading = false;

  // Hide inline panel, restore board.
  document.getElementById('refine-inline').classList.add('hidden');
  document.getElementById('board').classList.remove('hidden');
}

// renderRefineHistory populates the history section from task.refine_sessions.
function renderRefineHistory(task) {
  const section = document.getElementById('refine-history-section');
  const list = document.getElementById('refine-history-list');
  const sessions = (task.refine_sessions || []);
  if (sessions.length === 0) {
    section.classList.add('hidden');
    return;
  }
  section.classList.remove('hidden');
  list.innerHTML = sessions.map((s, i) => {
    const date = new Date(s.created_at).toLocaleString();
    const msgCount = (s.messages || []).length;
    const previewPrompt = s.start_prompt || '';
    const resultPrompt = s.result_prompt || '';
    return `<details class="refine-history-entry">
      <summary class="refine-history-summary">
        <span class="text-xs text-v-muted">#${i + 1} · ${escapeHtml(date)}</span>
        <span class="text-xs text-v-muted" style="margin-left:6px;">${msgCount} messages</span>
      </summary>
      <div style="padding:8px 0 0 0;">
        <div class="text-xs text-v-muted" style="margin-bottom:4px;">Starting prompt:</div>
        <pre class="code-block text-xs" style="white-space:pre-wrap;word-break:break-word;opacity:0.7;">${escapeHtml(previewPrompt)}</pre>
        ${resultPrompt ? `
        <div class="text-xs text-v-muted" style="margin-top:8px;margin-bottom:4px;">Result prompt:</div>
        <pre class="code-block text-xs" style="white-space:pre-wrap;word-break:break-word;">${escapeHtml(resultPrompt)}</pre>
        <button class="btn btn-ghost text-xs" style="margin-top:6px;" onclick="revertToHistoryPrompt(${i})">Revert to this version</button>
        ` : ''}
        ${(s.messages || []).length > 0 ? `
        <details style="margin-top:8px;">
          <summary class="text-xs text-v-muted" style="cursor:pointer;">View conversation (${s.messages.length} messages)</summary>
          <div style="margin-top:8px;display:flex;flex-direction:column;gap:6px;">
            ${(s.messages || []).map(m => `
              <div class="refine-msg refine-msg-${escapeHtml(m.role)}">
                <span class="refine-msg-role">${escapeHtml(m.role)}</span>
                <div class="refine-msg-body prose-content">${renderMarkdown(m.content)}</div>
              </div>
            `).join('')}
          </div>
        </details>
        ` : ''}
      </div>
    </details>`;
  }).join('');
}

// revertToHistoryPrompt reverts the proposed prompt textarea to a previous session's result.
function revertToHistoryPrompt(sessionIndex) {
  const task = tasks.find(t => t.id === refineTaskId);
  if (!task || !task.refine_sessions) return;
  const session = task.refine_sessions[sessionIndex];
  if (!session || !session.result_prompt) return;
  document.getElementById('refine-proposed-prompt').value = session.result_prompt;
  document.getElementById('refine-proposal-hint').classList.add('hidden');
}

// appendChatMessage renders a single message bubble in the chat panel.
function appendChatMessage(role, content) {
  const container = document.getElementById('refine-chat-messages');
  const div = document.createElement('div');
  div.className = 'refine-msg refine-msg-' + role;
  div.innerHTML = `
    <span class="refine-msg-role">${role === 'assistant' ? 'AI' : 'You'}</span>
    <div class="refine-msg-body prose-content">${renderMarkdown(content)}</div>
  `;
  container.appendChild(div);
  // Scroll to bottom.
  container.scrollTop = container.scrollHeight;
}

// refineInputKeydown sends on Ctrl/Cmd+Enter.
function refineInputKeydown(e) {
  if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') {
    e.preventDefault();
    sendRefineMessage();
  }
}

// sendRefineMessage reads the input field and calls sendRefineRequest.
function sendRefineMessage() {
  if (refineLoading) return;
  const input = document.getElementById('refine-chat-input');
  const message = input.value.trim();
  if (!message) return;
  input.value = '';

  // Append user message to chat immediately.
  appendChatMessage('user', message);
  sendRefineRequest(message);
}

// sendRefineRequest sends a chat turn to the backend and updates the UI.
// message is empty for the opening turn.
async function sendRefineRequest(message) {
  if (!refineTaskId) return;
  refineLoading = true;

  const sendBtn = document.getElementById('refine-send-btn');
  const input = document.getElementById('refine-chat-input');
  if (sendBtn) sendBtn.disabled = true;
  if (input) input.disabled = true;

  document.getElementById('refine-typing').classList.remove('hidden');

  // Build request body.  Conversation starts empty on first call.
  const body = {
    message: message,
    conversation: refineConversation.slice(), // send current history
  };

  // Append the user message to local conversation before sending.
  if (message) {
    refineConversation.push({ role: 'user', content: message });
  }

  try {
    const resp = await api(`/api/tasks/${refineTaskId}/refine`, {
      method: 'POST',
      body: JSON.stringify(body),
    });

    // Append assistant message to chat and local conversation.
    if (resp.message) {
      appendChatMessage('assistant', resp.message);
      refineConversation.push({ role: 'assistant', content: resp.message });
    }

    // If Claude proposed a refined prompt, populate the textarea.
    if (resp.refined_prompt) {
      document.getElementById('refine-proposed-prompt').value = resp.refined_prompt;
      document.getElementById('refine-proposal-hint').classList.remove('hidden');
    }

    // On first turn, the backend seeded the conversation with the task prompt.
    // Sync local conversation so subsequent calls have full context.
    if (refineConversation.length === 1 && !message) {
      // Opening turn: server used task.prompt as user[0], resp is assistant[0].
      refineConversation = [
        { role: 'user', content: document.getElementById('refine-current-prompt').textContent },
        { role: 'assistant', content: resp.message },
      ];
    }
  } catch (e) {
    appendChatMessage('assistant', '_(Error: ' + escapeHtml(e.message) + ')_');
  } finally {
    refineLoading = false;
    document.getElementById('refine-typing').classList.add('hidden');
    if (sendBtn) sendBtn.disabled = false;
    if (input) { input.disabled = false; input.focus(); }
  }
}

// applyRefinement POSTs the refined prompt (and updated settings) to the backend.
async function applyRefinement() {
  if (!refineTaskId) return;
  const newPrompt = document.getElementById('refine-proposed-prompt').value.trim();
  if (!newPrompt) {
    showAlert('The refined prompt cannot be empty.');
    return;
  }

  try {
    // Save model/timeout/worktrees settings alongside the prompt.
    const model = document.getElementById('refine-inline-model')?.value || '';
    const timeout = parseInt(document.getElementById('refine-inline-timeout')?.value, 10) || DEFAULT_TASK_TIMEOUT;
    const mountWorktrees = document.getElementById('refine-inline-mount-worktrees')?.checked || false;
    await api(`/api/tasks/${refineTaskId}`, {
      method: 'PATCH',
      body: JSON.stringify({ model, timeout, mount_worktrees: mountWorktrees }),
    });

    await api(`/api/tasks/${refineTaskId}/refine/apply`, {
      method: 'POST',
      body: JSON.stringify({
        prompt: newPrompt,
        conversation: refineConversation,
      }),
    });
    closeRefineModal();
    fetchTasks();
  } catch (e) {
    showAlert('Error applying refinement: ' + e.message);
  }
}
