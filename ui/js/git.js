// --- Git status stream ---

function startGitStream() {
  if (!activeWorkspaces || activeWorkspaces.length === 0) {
    renderWorkspaces();
    return;
  }
  if (gitStatusSource) gitStatusSource.close();

  // Follower tab: receive git status via BroadcastChannel relay.
  // Seed initial state with an HTTP fetch.
  if (!_sseIsLeader()) {
    gitStatusSource = null;
    _sseOnFollowerEvent("git-status", function (data) {
      gitRetryDelay = 1000;
      gitStatuses = data;
      renderWorkspaces();
    });
    api(Routes.git.status())
      .then(function (data) {
        if (Array.isArray(data)) {
          gitStatuses = data;
          renderWorkspaces();
        }
      })
      .catch(function (err) {
        console.error("git status fetch:", err);
      });
    return;
  }

  // Leader tab: open real EventSource and relay events to followers.
  gitStatusSource = new EventSource(withAuthToken(Routes.git.stream()));
  gitStatusSource.onmessage = function (e) {
    gitRetryDelay = 1000;
    try {
      var data = JSON.parse(e.data);
      gitStatuses = data;
      renderWorkspaces();
      _sseRelay("git-status", data);
    } catch (err) {
      console.error("git SSE parse error:", err);
    }
  };
  gitStatusSource.onerror = function () {
    if (gitStatusSource.readyState === EventSource.CLOSED) {
      gitStatusSource = null;
      var jittered = gitRetryDelay * (1 + Math.random()); // uniform [base, 2×base]
      setTimeout(startGitStream, jittered);
      gitRetryDelay = Math.min(gitRetryDelay * 2, 30000);
    }
  };
}

function remoteUrlToHttps(url) {
  if (!url) return null;
  url = url.trim();
  if (url.startsWith("http://") || url.startsWith("https://")) {
    return url.replace(/\.git$/, "");
  }
  // git@github.com:user/repo.git
  const sshMatch = url.match(/^git@([^:]+):(.+?)(?:\.git)?$/);
  if (sshMatch) return "https://" + sshMatch[1] + "/" + sshMatch[2];
  // ssh://git@github.com/user/repo.git
  const sshProtoMatch = url.match(
    /^ssh:\/\/(?:[^@]+@)?([^/]+)\/(.+?)(?:\.git)?$/,
  );
  if (sshProtoMatch)
    return "https://" + sshProtoMatch[1] + "/" + sshProtoMatch[2];
  return null;
}

function formatGitWorkspaceConflict(err, fallbackAction) {
  if (
    !err ||
    !Array.isArray(err.blocking_tasks) ||
    err.blocking_tasks.length === 0
  ) {
    return (err && err.error) || fallbackAction + " failed";
  }
  const lines = err.blocking_tasks.map(function (task) {
    const title = task.title || "(untitled task)";
    return (
      "- [" +
      String(task.status || "unknown").replace(/_/g, " ") +
      "] " +
      title +
      " (" +
      task.id +
      ")"
    );
  });
  return (
    ((err && err.error) || fallbackAction + " blocked") +
    "\n\nBlocking tasks:\n" +
    lines.join("\n")
  );
}

function setGitActionPending(btn, pendingLabel) {
  if (!btn) return function () {};
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
    method: "POST",
    headers: withAuthHeaders({ "Content-Type": "application/json" }, "POST"),
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
    const err = new Error((data && data.error) || text || "HTTP " + res.status);
    err.status = res.status;
    err.data = data;
    throw err;
  }

  return data;
}

async function openWorkspaceFolder(path) {
  try {
    await api(Routes.git.openFolder(), {
      method: "POST",
      body: JSON.stringify({ path: path }),
    });
  } catch (e) {
    showAlert("Failed to open folder: " + e.message);
  }
}

function renderWorkspaces() {
  renderStatusBarBranches();
  updateDocumentTitle();
  if (typeof updateStatusBar === "function") updateStatusBar();
}

function updateDocumentTitle() {
  if (!gitStatuses || gitStatuses.length === 0) {
    if (activeWorkspaces && activeWorkspaces.length > 0) {
      document.title = "Wallfacer";
    }
    return;
  }
  const names = gitStatuses.map((ws) => ws.name).filter(Boolean);
  if (names.length > 0) {
    document.title = "Wallfacer \u2014 " + names.join(", ");
  }
}

// renderStatusBarBranches — one group per workspace in the status bar's
// left slot: `⎇ branch` pill (opens branch dropdown) followed by ahead/
// behind badges and sync/push/rebase actions when relevant.
function renderStatusBarBranches() {
  const el = document.getElementById("status-bar-branches");
  if (!el) return;
  if (!gitStatuses || gitStatuses.length === 0) {
    el.innerHTML = "";
    return;
  }
  el.innerHTML = gitStatuses
    .map((ws, i) => {
      if (!ws.is_git_repo || !ws.branch) return "";
      const multi = gitStatuses.length > 1;
      const label = multi
        ? `${escapeHtml(ws.name)}:${escapeHtml(ws.branch)}`
        : escapeHtml(ws.branch);
      const title = `${ws.path || ws.name}\nBranch: ${ws.branch}`;
      const branchBtn =
        `<button type="button" class="status-bar-branch" data-ws-idx="${i}" ` +
        `onclick="toggleBranchDropdown(this, event)" title="${escapeHtml(title)}">` +
        `<span class="status-bar-branch__glyph">⎇</span>` +
        `<span class="status-bar-branch__name">${label}</span>` +
        `</button>`;

      // Upstream status + actions are only meaningful when there's a remote.
      let extras = "";
      if (ws.has_remote) {
        const behindBadge =
          ws.behind_count > 0
            ? `<span class="status-bar-branch__badge status-bar-branch__badge--behind" title="${ws.behind_count} commits behind upstream">${ws.behind_count}↓</span>`
            : "";
        const aheadBadge =
          ws.ahead_count > 0
            ? `<span class="status-bar-branch__badge status-bar-branch__badge--ahead" title="${ws.ahead_count} commits ahead of upstream">${ws.ahead_count}↑</span>`
            : "";
        const syncBtn =
          ws.behind_count > 0
            ? `<button type="button" data-ws-idx="${i}" onclick="syncWorkspace(this)" class="status-bar-branch__action status-bar-branch__action--sync" title="Pull ${ws.behind_count} commits from upstream">Sync</button>`
            : "";
        const pushBtn =
          ws.ahead_count > 0
            ? `<button type="button" data-ws-idx="${i}" onclick="pushWorkspace(this)" class="status-bar-branch__action status-bar-branch__action--push" title="Push ${ws.ahead_count} commits to upstream">Push</button>`
            : "";
        const rebaseBtn =
          ws.main_branch && ws.branch !== ws.main_branch
            ? `<button type="button" data-ws-idx="${i}" onclick="rebaseOnMain(this)" class="status-bar-branch__action status-bar-branch__action--rebase" title="Fetch origin/${escapeHtml(ws.main_branch)} and rebase current branch on top">${ws.behind_main_count > 0 ? ws.behind_main_count + "↓ " : ""}Rebase on ${escapeHtml(ws.main_branch)}</button>`
            : "";
        extras = behindBadge + aheadBadge + syncBtn + pushBtn + rebaseBtn;
      }
      return (
        `<span class="status-bar-branch-group">${branchBtn}${extras}</span>`
      );
    })
    .join("");
}

// --- Branch dropdown ---

function closeBranchDropdown() {
  const existing = document.querySelector(".branch-dropdown");
  if (existing) existing.remove();
}

function toggleBranchDropdown(btn, event) {
  event.stopPropagation();
  const existing = document.querySelector(".branch-dropdown");
  if (existing) {
    // If clicking the same button, just close
    if (existing._triggerBtn === btn) {
      existing.remove();
      return;
    }
    existing.remove();
  }
  const idx = parseInt(btn.getAttribute("data-ws-idx"), 10);
  const ws = gitStatuses[idx];
  if (!ws) return;

  const dropdown = document.createElement("div");
  dropdown.className = "branch-dropdown";
  dropdown._triggerBtn = btn;
  dropdown.innerHTML =
    '<div class="branch-dropdown-loading">Loading branches...</div>';

  // Position below the button
  const rect = btn.getBoundingClientRect();
  dropdown.style.position = "fixed";
  dropdown.style.top = rect.bottom + 4 + "px";
  dropdown.style.left = rect.left + "px";
  dropdown.style.zIndex = "9999";

  document.body.appendChild(dropdown);

  // Close on outside click
  setTimeout(() => {
    document.addEventListener("click", closeBranchDropdownOnClick);
  }, 0);

  // Load branches
  loadBranchesForDropdown(dropdown, idx, ws);
}

function closeBranchDropdownOnClick(e) {
  const dd = document.querySelector(".branch-dropdown");
  if (dd && !dd.contains(e.target)) {
    dd.remove();
    document.removeEventListener("click", closeBranchDropdownOnClick);
  }
}

async function loadBranchesForDropdown(dropdown, idx, ws) {
  try {
    const data = await api(
      Routes.git.branches() + "?workspace=" + encodeURIComponent(ws.path),
    );
    const current = data.current || ws.branch;
    const branches = data.branches || [];

    let html = '<div class="branch-dropdown-header">Switch branch</div>';
    html +=
      '<div class="branch-dropdown-search"><input type="text" placeholder="Filter or create branch..." class="branch-search-input" autocomplete="off" spellcheck="false"></div>';
    html += '<div class="branch-dropdown-list">';
    branches.forEach(function (b) {
      const isCurrent = b === current;
      html +=
        `<button class="branch-dropdown-item${isCurrent ? " current" : ""}" data-branch="${escapeHtml(b)}" data-ws-idx="${idx}" onclick="selectBranch(this)">` +
        (isCurrent
          ? '<svg width="12" height="12" viewBox="0 0 20 20" fill="currentColor" style="flex-shrink:0;color:var(--accent);"><path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd"/></svg>'
          : '<span style="width:12px;display:inline-block;"></span>') +
        `<span class="branch-dropdown-item-name">${escapeHtml(b)}</span></button>`;
    });
    html += "</div>";
    html += '<div class="branch-dropdown-footer">';
    html +=
      '<button class="branch-dropdown-create" data-ws-idx="' +
      idx +
      '" style="display:none;" onclick="createNewBranch(this)"><svg width="12" height="12" viewBox="0 0 20 20" fill="currentColor" style="flex-shrink:0;"><path fill-rule="evenodd" d="M10 3a1 1 0 011 1v5h5a1 1 0 110 2h-5v5a1 1 0 11-2 0v-5H4a1 1 0 110-2h5V4a1 1 0 011-1z" clip-rule="evenodd"/></svg><span></span></button>';
    html += "</div>";

    dropdown.innerHTML = html;

    // Set up search/filter behavior
    const input = dropdown.querySelector(".branch-search-input");
    const list = dropdown.querySelector(".branch-dropdown-list");
    const createBtn = dropdown.querySelector(".branch-dropdown-create");
    input.focus();

    input.addEventListener("input", function () {
      const q = input.value.trim().toLowerCase();
      const items = list.querySelectorAll(".branch-dropdown-item");
      let anyVisible = false;
      let exactMatch = false;
      items.forEach(function (item) {
        const name = item.getAttribute("data-branch").toLowerCase();
        const show = !q || name.includes(q);
        item.style.display = show ? "" : "none";
        if (show) anyVisible = true;
        if (name === q) exactMatch = true;
      });

      // Show "Create branch" option when there's text and no exact match
      if (q && !exactMatch) {
        createBtn.style.display = "";
        createBtn.querySelector("span").textContent =
          'Create branch "' + input.value.trim() + '"';
        createBtn.setAttribute("data-new-branch", input.value.trim());
      } else {
        createBtn.style.display = "none";
      }
    });

    // Handle Enter key to create branch when create button is visible
    input.addEventListener("keydown", function (e) {
      if (e.key === "Enter") {
        e.preventDefault();
        if (createBtn.style.display !== "none") {
          createNewBranch(createBtn);
        }
      } else if (e.key === "Escape") {
        closeBranchDropdown();
        document.removeEventListener("click", closeBranchDropdownOnClick);
      }
    });
  } catch (e) {
    dropdown.innerHTML =
      '<div class="branch-dropdown-loading" style="color:var(--text-error);">Failed to load branches</div>';
    console.error("Failed to load branches:", e);
  }
}

async function selectBranch(item) {
  const idx = parseInt(item.getAttribute("data-ws-idx"), 10);
  const ws = gitStatuses[idx];
  const branch = item.getAttribute("data-branch");
  if (!ws || branch === ws.branch) {
    closeBranchDropdown();
    document.removeEventListener("click", closeBranchDropdownOnClick);
    return;
  }

  const restore = setGitActionPending(item);
  try {
    await requestGitWorkspaceMutation(Routes.git.checkout(), {
      workspace: ws.path,
      branch: branch,
    });
    closeBranchDropdown();
    document.removeEventListener("click", closeBranchDropdownOnClick);
  } catch (e) {
    if (e.status === 409) {
      showAlert(
        "Branch switch blocked:\n\n" +
          formatGitWorkspaceConflict(e.data, "Branch switch"),
      );
    } else {
      showAlert("Branch switch failed: " + e.message);
    }
    restore();
  }
}

async function createNewBranch(btn) {
  const idx = parseInt(btn.getAttribute("data-ws-idx"), 10);
  const ws = gitStatuses[idx];
  const branch = btn.getAttribute("data-new-branch");
  if (!ws || !branch) return;

  const restore = setGitActionPending(btn);
  try {
    await requestGitWorkspaceMutation(Routes.git.createBranch(), {
      workspace: ws.path,
      branch: branch,
    });
    closeBranchDropdown();
    document.removeEventListener("click", closeBranchDropdownOnClick);
  } catch (e) {
    if (e.status === 409) {
      showAlert(
        "Create branch blocked:\n\n" +
          formatGitWorkspaceConflict(e.data, "Create branch"),
      );
    } else {
      showAlert("Failed to create branch: " + e.message);
    }
    restore();
  }
}

async function pushWorkspace(btn) {
  const idx = parseInt(btn.getAttribute("data-ws-idx"), 10);
  const ws = gitStatuses[idx];
  if (!ws) return;
  btn.disabled = true;
  btn.textContent = "...";
  try {
    await api(Routes.git.push(), {
      method: "POST",
      body: JSON.stringify({ workspace: ws.path }),
    });
  } catch (e) {
    showAlert(
      "Push failed: " +
        e.message +
        (e.message.includes("non-fast-forward")
          ? "\n\nTip: Use Sync to rebase onto upstream first."
          : ""),
    );
    btn.disabled = false;
    btn.textContent = "Push";
  }
}

async function syncWorkspace(btn) {
  const idx = parseInt(btn.getAttribute("data-ws-idx"), 10);
  const ws = gitStatuses[idx];
  if (!ws) return;
  const restore = setGitActionPending(btn, "...");
  try {
    await requestGitWorkspaceMutation(Routes.git.sync(), {
      workspace: ws.path,
    });
    // Status stream will update behind_count automatically.
  } catch (e) {
    if (e.status === 409 && e.data && Array.isArray(e.data.blocking_tasks)) {
      showAlert(
        "Sync blocked:\n\n" + formatGitWorkspaceConflict(e.data, "Sync"),
      );
    } else if (e.message && e.message.includes("rebase conflict")) {
      showAlert(
        "Sync failed: rebase conflict in " +
          ws.name +
          ".\n\nResolve the conflict manually in:\n" +
          ws.path,
      );
    } else {
      showAlert("Sync failed: " + e.message);
    }
    restore();
  }
}

async function rebaseOnMain(btn) {
  const idx = parseInt(btn.getAttribute("data-ws-idx"), 10);
  const ws = gitStatuses[idx];
  if (!ws) return;
  const restore = setGitActionPending(btn, "...");
  try {
    await requestGitWorkspaceMutation(Routes.git.rebaseOnMain(), {
      workspace: ws.path,
    });
    // Status stream will pick up the updated state.
  } catch (e) {
    if (e.status === 409 && e.data && Array.isArray(e.data.blocking_tasks)) {
      showAlert(
        "Rebase blocked:\n\n" + formatGitWorkspaceConflict(e.data, "Rebase"),
      );
    } else if (e.message && e.message.includes("rebase conflict")) {
      showAlert(
        "Rebase failed: conflict in " +
          ws.name +
          ".\n\nResolve the conflict manually in:\n" +
          ws.path,
      );
    } else {
      showAlert("Rebase failed: " + e.message);
    }
    restore();
  }
}
