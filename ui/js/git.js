// --- Git status stream ---

function startGitStream() {
  if (gitStatusSource) gitStatusSource.close();
  gitStatusSource = new EventSource('/api/git/stream');
  gitStatusSource.onmessage = function(e) {
    gitRetryDelay = 1000;
    try {
      gitStatuses = JSON.parse(e.data);
      renderWorkspaces();
    } catch (err) {
      console.error('git SSE parse error:', err);
    }
  };
  gitStatusSource.onerror = function() {
    if (gitStatusSource.readyState === EventSource.CLOSED) {
      gitStatusSource = null;
      setTimeout(startGitStream, gitRetryDelay);
      gitRetryDelay = Math.min(gitRetryDelay * 2, 30000);
    }
  };
}

function renderWorkspaces() {
  const el = document.getElementById('workspace-list');
  if (!gitStatuses || gitStatuses.length === 0) return;
  el.innerHTML = gitStatuses.map((ws, i) => {
    if (!ws.is_git_repo || !ws.has_remote) {
      return `<span title="${escapeHtml(ws.path)}" style="font-size: 11px; padding: 2px 8px; border-radius: 4px; background: var(--bg-input); color: var(--text-muted); border: 1px solid var(--border);">${escapeHtml(ws.name)}</span>`;
    }
    const branchLabel = ws.branch ? ` <span style="opacity:0.55;">${escapeHtml(ws.branch)}</span>` : '';
    const aheadBadge = ws.ahead_count > 0
      ? `<span style="background:var(--accent);color:#fff;border-radius:3px;padding:0 5px;font-size:10px;font-weight:600;line-height:17px;">${ws.ahead_count}↑</span>`
      : '';
    const behindBadge = ws.behind_count > 0
      ? `<span style="background:var(--text-muted);color:#fff;border-radius:3px;padding:0 5px;font-size:10px;font-weight:600;line-height:17px;">${ws.behind_count}↓</span>`
      : '';
    const syncBtn = ws.behind_count > 0
      ? `<button data-ws-idx="${i}" onclick="syncWorkspace(this)" style="background:var(--text-muted);color:#fff;border:none;border-radius:3px;padding:1px 7px;font-size:10px;font-weight:500;cursor:pointer;line-height:17px;">Sync</button>`
      : '';
    const pushBtn = ws.ahead_count > 0
      ? `<button data-ws-idx="${i}" onclick="pushWorkspace(this)" style="background:var(--accent);color:#fff;border:none;border-radius:3px;padding:1px 7px;font-size:10px;font-weight:500;cursor:pointer;line-height:17px;">Push</button>`
      : '';
    return `<span title="${escapeHtml(ws.path)}" style="display:inline-flex;align-items:center;gap:4px;font-size:11px;padding:2px 6px 2px 8px;border-radius:4px;background:var(--bg-input);color:var(--text-muted);border:1px solid var(--border);">${escapeHtml(ws.name)}${branchLabel}${behindBadge}${aheadBadge}${syncBtn}${pushBtn}</span>`;
  }).join('');
}

async function pushWorkspace(btn) {
  const idx = parseInt(btn.getAttribute('data-ws-idx'), 10);
  const ws = gitStatuses[idx];
  if (!ws) return;
  btn.disabled = true;
  btn.textContent = '...';
  try {
    await api('/api/git/push', { method: 'POST', body: JSON.stringify({ workspace: ws.path }) });
  } catch (e) {
    showAlert('Push failed: ' + e.message + (e.message.includes('non-fast-forward') ? '\n\nTip: Use Sync to rebase onto upstream first.' : ''));
    btn.disabled = false;
    btn.textContent = 'Push';
  }
}

async function syncWorkspace(btn) {
  const idx = parseInt(btn.getAttribute('data-ws-idx'), 10);
  const ws = gitStatuses[idx];
  if (!ws) return;
  btn.disabled = true;
  btn.textContent = '...';
  try {
    await api('/api/git/sync', { method: 'POST', body: JSON.stringify({ workspace: ws.path }) });
    // Status stream will update behind_count automatically.
  } catch (e) {
    if (e.message && e.message.includes('rebase conflict')) {
      showAlert('Sync failed: rebase conflict in ' + ws.name + '.\n\nResolve the conflict manually in:\n' + ws.path);
    } else {
      showAlert('Sync failed: ' + e.message);
    }
    btn.disabled = false;
    btn.textContent = 'Sync';
  }
}
