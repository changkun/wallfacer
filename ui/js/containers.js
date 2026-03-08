// --- Container Monitor ---

var containerMonitorInterval = null;

function showContainerMonitor(e) {
  if (e) e.stopPropagation();
  closeSettings();

  var modal = document.getElementById('container-monitor-modal');
  modal.classList.remove('hidden');
  modal.style.display = 'flex';

  // Show loading, hide others.
  setContainerMonitorState('loading');
  fetchContainers();

  // Auto-refresh every 5 seconds while the modal is open.
  containerMonitorInterval = setInterval(fetchContainers, 5000);
}

function closeContainerMonitor() {
  var modal = document.getElementById('container-monitor-modal');
  modal.classList.add('hidden');
  modal.style.display = '';
  if (containerMonitorInterval) {
    clearInterval(containerMonitorInterval);
    containerMonitorInterval = null;
  }
}

function refreshContainerMonitor() {
  setContainerMonitorState('loading');
  fetchContainers();
}

function setContainerMonitorState(state) {
  var loading = document.getElementById('container-monitor-loading');
  var error   = document.getElementById('container-monitor-error');
  var empty   = document.getElementById('container-monitor-empty');
  var table   = document.getElementById('container-monitor-table-wrap');

  loading.style.display = state === 'loading' ? 'flex' : 'none';
  error.classList.toggle('hidden', state !== 'error');
  empty.classList.toggle('hidden', state !== 'empty');
  table.classList.toggle('hidden', state !== 'table');
}

function fetchContainers() {
  fetch('/api/containers')
    .then(function(res) {
      return res.json().then(function(data) {
        return { ok: res.ok, data: data };
      });
    })
    .then(function(result) {
      if (!result.ok) {
        document.getElementById('container-monitor-error').textContent =
          result.data.error || 'Unknown error';
        setContainerMonitorState('error');
        return;
      }
      renderContainers(result.data);
    })
    .catch(function(err) {
      document.getElementById('container-monitor-error').textContent = String(err);
      setContainerMonitorState('error');
    });
}

function renderContainers(containers) {
  var updated = document.getElementById('container-monitor-updated');
  updated.textContent = 'Last refreshed: ' + new Date().toLocaleTimeString();

  if (!containers || containers.length === 0) {
    setContainerMonitorState('empty');
    return;
  }

  // Build a task-id → task map for linking.
  var taskMap = {};
  if (window.state && state.tasks) {
    state.tasks.forEach(function(t) { taskMap[t.id] = t; });
  }

  var tbody = document.getElementById('container-monitor-tbody');
  tbody.innerHTML = '';

  containers.forEach(function(c) {
    var tr = document.createElement('tr');
    tr.style.cssText = 'border-bottom: 1px solid var(--border); transition: background 0.1s;';
    tr.addEventListener('mouseenter', function() { tr.style.background = 'var(--bg-raised)'; });
    tr.addEventListener('mouseleave', function() { tr.style.background = ''; });

    var shortID = c.id ? c.id.substring(0, 12) : '—';
    var stateColor = containerStateColor(c.state);
    var stateLabel = c.state || '—';

    // Task cell: show title with hover tooltip for full prompt.
    // Prefer server-enriched task_title, then fall back to window.state lookup.
    var taskCell = '';
    if (c.task_id) {
      var task = taskMap[c.task_id];
      // Prefer the server-provided title (always fresh); fall back to state lookup.
      var displayTitle = c.task_title ||
        (task ? (task.title || task.prompt || c.task_id) : null);
      // Build tooltip from the full task prompt (for hover detail).
      var tooltipText = task ? (task.prompt || '') : (c.task_title || '');

      if (displayTitle) {
        var badgeClass = task ? 'badge badge-' + (task.status || 'backlog') : '';
        var badgeHtml = task
          ? '<span class="' + badgeClass + '" style="margin-right:6px;">' + escapeHtml(task.status) + '</span>'
          : '';
        taskCell = '<span title="' + escapeHtml(tooltipText) + '" style="cursor:default;">' +
          badgeHtml +
          '<span style="color:var(--text-primary);">' + escapeHtml(displayTitle) + '</span>' +
          '</span>';
      } else {
        taskCell = '<span style="font-family:monospace;color:var(--text-muted);">' +
          escapeHtml(c.task_id.substring(0, 8)) + '</span>';
      }
    } else {
      taskCell = '<span style="color:var(--text-muted);">—</span>';
    }

    // Created: format unix timestamp.
    var createdStr = '—';
    if (c.created_at && c.created_at > 0) {
      createdStr = relativeTime(c.created_at * 1000);
    }

    tr.innerHTML = [
      '<td style="padding:8px 10px;font-family:monospace;color:var(--text-secondary);white-space:nowrap;">' + escapeHtml(shortID) + '</td>',
      '<td style="padding:8px 10px;max-width:260px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;">' + taskCell + '</td>',
      '<td style="padding:8px 10px;font-family:monospace;color:var(--text-secondary);white-space:nowrap;max-width:220px;overflow:hidden;text-overflow:ellipsis;" title="' + escapeHtml(c.name || '') + '">' + escapeHtml(c.name || '—') + '</td>',
      '<td style="padding:8px 10px;white-space:nowrap;"><span style="display:inline-flex;align-items:center;gap:5px;"><span style="width:7px;height:7px;border-radius:50%;background:' + stateColor + ';flex-shrink:0;"></span>' + escapeHtml(stateLabel) + '</span></td>',
      '<td style="padding:8px 10px;color:var(--text-secondary);white-space:nowrap;">' + escapeHtml(c.status || '—') + '</td>',
      '<td style="padding:8px 10px;color:var(--text-secondary);white-space:nowrap;">' + createdStr + '</td>',
    ].join('');

    tbody.appendChild(tr);
  });

  setContainerMonitorState('table');
}

function containerStateColor(state) {
  switch ((state || '').toLowerCase()) {
    case 'running':  return '#45b87a';
    case 'exited':   return '#9c9890';
    case 'paused':   return '#d4a030';
    case 'created':  return '#6da0dc';
    case 'dead':     return '#d46868';
    default:         return '#9c9890';
  }
}

function relativeTime(ms) {
  var diff = Date.now() - ms;
  var s = Math.floor(diff / 1000);
  if (s < 60)  return s + 's ago';
  var m = Math.floor(s / 60);
  if (m < 60)  return m + 'm ago';
  var h = Math.floor(m / 60);
  if (h < 24)  return h + 'h ago';
  var d = Math.floor(h / 24);
  return d + 'd ago';
}

// Close modal on backdrop click.
document.getElementById('container-monitor-modal').addEventListener('click', function(e) {
  if (e.target === this) closeContainerMonitor();
});
