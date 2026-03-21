// --- Git status stream ---

function startGitStream() {
  if (!activeWorkspaces || activeWorkspaces.length === 0) {
    renderWorkspaces();
    return;
  }
  if (gitStatusSource) gitStatusSource.close();
  gitStatusSource = new EventSource(withAuthToken(Routes.git.stream()));
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

function remoteUrlToHttps(url) {
  if (!url) return null;
  url = url.trim();
  if (url.startsWith('http://') || url.startsWith('https://')) {
    return url.replace(/\.git$/, '');
  }
  // git@github.com:user/repo.git
  const sshMatch = url.match(/^git@([^:]+):(.+?)(?:\.git)?$/);
  if (sshMatch) return 'https://' + sshMatch[1] + '/' + sshMatch[2];
  // ssh://git@github.com/user/repo.git
  const sshProtoMatch = url.match(/^ssh:\/\/(?:[^@]+@)?([^/]+)\/(.+?)(?:\.git)?$/);
  if (sshProtoMatch) return 'https://' + sshProtoMatch[1] + '/' + sshProtoMatch[2];
  return null;
}

function formatGitWorkspaceConflict(err, fallbackAction) {
  if (!err || !Array.isArray(err.blocking_tasks) || err.blocking_tasks.length === 0) {
    return (err && err.error) || (fallbackAction + ' failed');
  }
  const lines = err.blocking_tasks.map(function(task) {
    const title = task.title || '(untitled task)';
    return '- [' + String(task.status || 'unknown').replace(/_/g, ' ') + '] ' + title + ' (' + task.id + ')';
  });
  return ((err && err.error) || (fallbackAction + ' blocked')) + '\n\nBlocking tasks:\n' + lines.join('\n');
}

function setGitActionPending(btn, pendingLabel) {
  if (!btn) return function() {};
  const originalDisabled = !!btn.disabled;
  const originalText = btn.textContent;
  btn.disabled = true;
  if (pendingLabel) btn.textContent = pendingLabel;
  return function restore() {
    btn.disabled = originalDisabled;
    btn.textContent = originalText;
  };
}

async function requestGitWorkspaceMutation(path, payload) {
  const res = await fetch(path, {
    method: 'POST',
    headers: withAuthHeaders({ 'Content-Type': 'application/json' }, 'POST'),
    body: JSON.stringify(payload),
  });

  if (res.status === 204) return null;

  const text = await res.text();
  let data = null;
  if (text) {
    try {
      data = JSON.parse(text);
    } catch (_) {
      data = null;
    }
  }

  if (!res.ok) {
    const err = new Error((data && data.error) || text || ('HTTP ' + res.status));
    err.status = res.status;
    err.data = data;
    throw err;
  }

  return data;
}

async function openWorkspaceFolder(path) {
  try {
    await api(Routes.git.openFolder(), { method: 'POST', body: JSON.stringify({ path: path }) });
  } catch (e) {
    showAlert('Failed to open folder: ' + e.message);
  }
}

function renderWorkspaces() {
  const el = document.getElementById('workspace-group-tab-workspaces');
  if (!el) return;
  if (!gitStatuses || gitStatuses.length === 0) {
    el.innerHTML = (activeWorkspaces || []).map((path) => {
      const parts = String(path || '').replace(/[\\/]+$/, '').split(/[\\/]/);
      const name = parts[parts.length - 1] || path;
      return `<span title="${escapeHtml(path)}" style="font-size:11px;padding:2px 8px;border-radius:999px;background:var(--bg-input);color:var(--text-muted);border:1px solid var(--border);">${escapeHtml(name)}</span>`;
    }).join('');
    if (activeWorkspaces && activeWorkspaces.length > 0) {
      document.title = 'Wallfacer';
    }
    return;
  }
  // Update browser tab title with workspace names
  const names = gitStatuses.map(ws => ws.name).filter(Boolean);
  if (names.length > 0) {
    document.title = 'Wallfacer \u2014 ' + names.join(', ');
  }
  el.innerHTML = gitStatuses.map((ws, i) => {
    const httpsUrl = remoteUrlToHttps(ws.remote_url);
    const nameEl = httpsUrl
      ? `<a href="${escapeHtml(httpsUrl)}" target="_blank" rel="noopener noreferrer" style="color:inherit;text-decoration:none;" title="Open ${escapeHtml(httpsUrl)}">${escapeHtml(ws.name)}</a>`
      : `<button onclick="openWorkspaceFolder(${JSON.stringify(ws.path)})" style="background:none;border:none;padding:0;cursor:pointer;color:inherit;font:inherit;" title="Open in file manager">${escapeHtml(ws.name)}</button>`;

    if (!ws.is_git_repo) {
      return `<span title="${escapeHtml(ws.path)}" style="display:inline-flex;align-items:center;gap:4px;font-size: 11px; padding: 2px 8px; border-radius: 4px; background: var(--bg-input); color: var(--text-muted); border: 1px solid var(--border);">${nameEl} <span style="font-size:9px;opacity:0.7;">(not a git repo)</span></span>`;
    }
    if (!ws.has_remote) {
      return `<span title="${escapeHtml(ws.path)}" style="font-size: 11px; padding: 2px 8px; border-radius: 4px; background: var(--bg-input); color: var(--text-muted); border: 1px solid var(--border);">${nameEl}</span>`;
    }
    const branchBtn = ws.branch
      ? ` <button class="branch-switcher-btn" data-ws-idx="${i}" onclick="toggleBranchDropdown(this, event)" title="Switch branch">`
        + `<svg width="12" height="12" viewBox="0 0 16 16" fill="currentColor" style="flex-shrink:0;"><path d="M9.5 3.25a2.25 2.25 0 1 1 3 2.122V6A2.5 2.5 0 0 1 10 8.5H6a1 1 0 0 0-1 1v1.128a2.251 2.251 0 1 1-1.5 0V5.372a2.25 2.25 0 1 1 1.5 0v1.836A2.493 2.493 0 0 1 6 7h4a1 1 0 0 0 1-1v-.628A2.25 2.25 0 0 1 9.5 3.25Z"></path></svg>`
        + `<span class="branch-name">${escapeHtml(ws.branch)}</span>`
        + `<svg width="10" height="10" viewBox="0 0 20 20" fill="currentColor" style="flex-shrink:0;opacity:0.6;"><path fill-rule="evenodd" d="M5.23 7.21a.75.75 0 011.06.02L10 11.168l3.71-3.938a.75.75 0 111.08 1.04l-4.25 4.5a.75.75 0 01-1.08 0l-4.25-4.5a.75.75 0 01.02-1.06z" clip-rule="evenodd"/></svg>`
        + `</button>`
      : '';
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
    const rebaseMainBtn = (ws.branch && ws.main_branch && ws.branch !== ws.main_branch)
      ? `<button data-ws-idx="${i}" onclick="rebaseOnMain(this)" style="background:#7c3aed;color:#fff;border:none;border-radius:3px;padding:1px 7px;font-size:10px;font-weight:500;cursor:pointer;line-height:17px;" title="Fetch origin/${escapeHtml(ws.main_branch)} and rebase current branch on top">${ws.behind_main_count > 0 ? ws.behind_main_count + '↓ ' : ''}Rebase on ${escapeHtml(ws.main_branch)}</button>`
      : '';
    return `<span title="${escapeHtml(ws.path)}" style="display:inline-flex;align-items:center;gap:4px;font-size:11px;padding:2px 6px 2px 8px;border-radius:4px;background:var(--bg-input);color:var(--text-muted);border:1px solid var(--border);position:relative;">${nameEl}${branchBtn}${behindBadge}${aheadBadge}${syncBtn}${pushBtn}${rebaseMainBtn}</span>`;
  }).join('');
}

// --- Branch dropdown ---

function closeBranchDropdown() {
  const existing = document.querySelector('.branch-dropdown');
  if (existing) existing.remove();
}

function toggleBranchDropdown(btn, event) {
  event.stopPropagation();
  const existing = document.querySelector('.branch-dropdown');
  if (existing) {
    // If clicking the same button, just close
    if (existing._triggerBtn === btn) {
      existing.remove();
      return;
    }
    existing.remove();
  }
  const idx = parseInt(btn.getAttribute('data-ws-idx'), 10);
  const ws = gitStatuses[idx];
  if (!ws) return;

  const dropdown = document.createElement('div');
  dropdown.className = 'branch-dropdown';
  dropdown._triggerBtn = btn;
  dropdown.innerHTML = '<div class="branch-dropdown-loading">Loading branches...</div>';

  // Position below the button
  const rect = btn.getBoundingClientRect();
  dropdown.style.position = 'fixed';
  dropdown.style.top = (rect.bottom + 4) + 'px';
  dropdown.style.left = rect.left + 'px';
  dropdown.style.zIndex = '9999';

  document.body.appendChild(dropdown);

  // Close on outside click
  setTimeout(() => {
    document.addEventListener('click', closeBranchDropdownOnClick);
  }, 0);

  // Load branches
  loadBranchesForDropdown(dropdown, idx, ws);
}

function closeBranchDropdownOnClick(e) {
  const dd = document.querySelector('.branch-dropdown');
  if (dd && !dd.contains(e.target)) {
    dd.remove();
    document.removeEventListener('click', closeBranchDropdownOnClick);
  }
}

async function loadBranchesForDropdown(dropdown, idx, ws) {
  try {
    const data = await api(Routes.git.branches() + '?workspace=' + encodeURIComponent(ws.path));
    const current = data.current || ws.branch;
    const branches = data.branches || [];

    let html = '<div class="branch-dropdown-header">Switch branch</div>';
    html += '<div class="branch-dropdown-search"><input type="text" placeholder="Filter or create branch..." class="branch-search-input" autocomplete="off" spellcheck="false"></div>';
    html += '<div class="branch-dropdown-list">';
    branches.forEach(function(b) {
      const isCurrent = b === current;
      html += `<button class="branch-dropdown-item${isCurrent ? ' current' : ''}" data-branch="${escapeHtml(b)}" data-ws-idx="${idx}" onclick="selectBranch(this)">`
        + (isCurrent ? '<svg width="12" height="12" viewBox="0 0 20 20" fill="currentColor" style="flex-shrink:0;color:var(--accent);"><path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd"/></svg>' : '<span style="width:12px;display:inline-block;"></span>')
        + `<span class="branch-dropdown-item-name">${escapeHtml(b)}</span></button>`;
    });
    html += '</div>';
    html += '<div class="branch-dropdown-footer">';
    html += '<button class="branch-dropdown-create" data-ws-idx="' + idx + '" style="display:none;" onclick="createNewBranch(this)"><svg width="12" height="12" viewBox="0 0 20 20" fill="currentColor" style="flex-shrink:0;"><path fill-rule="evenodd" d="M10 3a1 1 0 011 1v5h5a1 1 0 110 2h-5v5a1 1 0 11-2 0v-5H4a1 1 0 110-2h5V4a1 1 0 011-1z" clip-rule="evenodd"/></svg><span></span></button>';
    html += '</div>';

    dropdown.innerHTML = html;

    // Set up search/filter behavior
    const input = dropdown.querySelector('.branch-search-input');
    const list = dropdown.querySelector('.branch-dropdown-list');
    const createBtn = dropdown.querySelector('.branch-dropdown-create');
    input.focus();

    input.addEventListener('input', function() {
      const q = input.value.trim().toLowerCase();
      const items = list.querySelectorAll('.branch-dropdown-item');
      let anyVisible = false;
      let exactMatch = false;
      items.forEach(function(item) {
        const name = item.getAttribute('data-branch').toLowerCase();
        const show = !q || name.includes(q);
        item.style.display = show ? '' : 'none';
        if (show) anyVisible = true;
        if (name === q) exactMatch = true;
      });

      // Show "Create branch" option when there's text and no exact match
      if (q && !exactMatch) {
        createBtn.style.display = '';
        createBtn.querySelector('span').textContent = 'Create branch "' + input.value.trim() + '"';
        createBtn.setAttribute('data-new-branch', input.value.trim());
      } else {
        createBtn.style.display = 'none';
      }
    });

    // Handle Enter key to create branch when create button is visible
    input.addEventListener('keydown', function(e) {
      if (e.key === 'Enter') {
        e.preventDefault();
        if (createBtn.style.display !== 'none') {
          createNewBranch(createBtn);
        }
      } else if (e.key === 'Escape') {
        closeBranchDropdown();
        document.removeEventListener('click', closeBranchDropdownOnClick);
      }
    });
  } catch (e) {
    dropdown.innerHTML = '<div class="branch-dropdown-loading" style="color:var(--text-error);">Failed to load branches</div>';
    console.error('Failed to load branches:', e);
  }
}

async function selectBranch(item) {
  const idx = parseInt(item.getAttribute('data-ws-idx'), 10);
  const ws = gitStatuses[idx];
  const branch = item.getAttribute('data-branch');
  if (!ws || branch === ws.branch) {
    closeBranchDropdown();
    document.removeEventListener('click', closeBranchDropdownOnClick);
    return;
  }

  const restore = setGitActionPending(item);
  try {
    await requestGitWorkspaceMutation(Routes.git.checkout(), { workspace: ws.path, branch: branch });
    closeBranchDropdown();
    document.removeEventListener('click', closeBranchDropdownOnClick);
  } catch (e) {
    if (e.status === 409) {
      showAlert('Branch switch blocked:\n\n' + formatGitWorkspaceConflict(e.data, 'Branch switch'));
    } else {
      showAlert('Branch switch failed: ' + e.message);
    }
    restore();
  }
}

async function createNewBranch(btn) {
  const idx = parseInt(btn.getAttribute('data-ws-idx'), 10);
  const ws = gitStatuses[idx];
  const branch = btn.getAttribute('data-new-branch');
  if (!ws || !branch) return;

  const restore = setGitActionPending(btn);
  try {
    await requestGitWorkspaceMutation(Routes.git.createBranch(), { workspace: ws.path, branch: branch });
    closeBranchDropdown();
    document.removeEventListener('click', closeBranchDropdownOnClick);
  } catch (e) {
    if (e.status === 409) {
      showAlert('Create branch blocked:\n\n' + formatGitWorkspaceConflict(e.data, 'Create branch'));
    } else {
      showAlert('Failed to create branch: ' + e.message);
    }
    restore();
  }
}

async function pushWorkspace(btn) {
  const idx = parseInt(btn.getAttribute('data-ws-idx'), 10);
  const ws = gitStatuses[idx];
  if (!ws) return;
  btn.disabled = true;
  btn.textContent = '...';
  try {
    await api(Routes.git.push(), { method: 'POST', body: JSON.stringify({ workspace: ws.path }) });
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
  const restore = setGitActionPending(btn, '...');
  try {
    await requestGitWorkspaceMutation(Routes.git.sync(), { workspace: ws.path });
    // Status stream will update behind_count automatically.
  } catch (e) {
    if (e.status === 409 && e.data && Array.isArray(e.data.blocking_tasks)) {
      showAlert('Sync blocked:\n\n' + formatGitWorkspaceConflict(e.data, 'Sync'));
    } else if (e.message && e.message.includes('rebase conflict')) {
      showAlert('Sync failed: rebase conflict in ' + ws.name + '.\n\nResolve the conflict manually in:\n' + ws.path);
    } else {
      showAlert('Sync failed: ' + e.message);
    }
    restore();
  }
}

async function rebaseOnMain(btn) {
  const idx = parseInt(btn.getAttribute('data-ws-idx'), 10);
  const ws = gitStatuses[idx];
  if (!ws) return;
  const restore = setGitActionPending(btn, '...');
  try {
    await requestGitWorkspaceMutation(Routes.git.rebaseOnMain(), { workspace: ws.path });
    // Status stream will pick up the updated state.
  } catch (e) {
    if (e.status === 409 && e.data && Array.isArray(e.data.blocking_tasks)) {
      showAlert('Rebase blocked:\n\n' + formatGitWorkspaceConflict(e.data, 'Rebase'));
    } else if (e.message && e.message.includes('rebase conflict')) {
      showAlert('Rebase failed: conflict in ' + ws.name + '.\n\nResolve the conflict manually in:\n' + ws.path);
    } else {
      showAlert('Rebase failed: ' + e.message);
    }
    restore();
  }
}
