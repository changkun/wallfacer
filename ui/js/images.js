// --- Sandbox Image Management ---

// Track active pulls so we can reconnect when the settings panel reopens.
var _activePulls = {}; // { sandbox: { pullId, source } }

// Loads and renders sandbox image status in Settings > Sandbox > Container Images.
async function loadImageStatus() {
  var container = document.getElementById('sandbox-images-list');
  if (!container) return;

  container.innerHTML = '<div style="font-size:12px; color:var(--text-muted);">Loading...</div>';

  try {
    var data = await api(Routes.images.status());
    container.innerHTML = '';
    if (!Array.isArray(data.images)) return;

    data.images.forEach(function(img) {
      var sb = String(img.sandbox);
      var active = _activePulls[sb];
      var isPulling = active && active.source && active.source.readyState !== EventSource.CLOSED;

      var row = document.createElement('div');
      row.style.cssText = 'display:flex; align-items:center; gap:8px; padding:8px 10px; border:1px solid var(--border); border-radius:6px; font-size:12px;';

      var badge;
      if (isPulling) {
        badge = '<span style="display:inline-block;padding:1px 6px;border-radius:4px;background:#fef9c3;color:#854d0e;font-size:11px;font-weight:600;">Pulling\u2026</span>';
      } else if (img.cached) {
        badge = '<span style="display:inline-block;padding:1px 6px;border-radius:4px;background:#dcfce7;color:#166534;font-size:11px;font-weight:600;">Cached</span>';
      } else {
        badge = '<span style="display:inline-block;padding:1px 6px;border-radius:4px;background:#fef2f2;color:#991b1b;font-size:11px;font-weight:600;">Missing</span>';
      }

      var info = '<div style="flex:1;min-width:0;">'
        + '<div style="font-weight:600;text-transform:capitalize;">' + escapeHtml(sb) + '</div>'
        + '<div style="font-size:11px;color:var(--text-muted);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" title="' + escapeHtml(img.image) + '">' + escapeHtml(img.image) + '</div>'
        + (img.size ? '<div style="font-size:11px;color:var(--text-muted);">Size: ' + escapeHtml(img.size) + '</div>' : '')
        + '</div>';

      var btnLabel = isPulling ? 'Pulling\u2026' : (img.cached ? 'Re-pull' : 'Pull');
      var btnId = 'pull-btn-' + sb;
      var progressId = 'pull-progress-' + sb;

      var deleteBtn = '';
      if (img.cached && !isPulling) {
        deleteBtn = ' <button type="button" class="btn-sm" style="white-space:nowrap;color:var(--text-error,#dc2626);" onclick="deleteSandboxImage(\'' + escapeHtml(sb) + '\')">Delete</button>';
      }

      row.innerHTML = info + badge
        + ' <button id="' + btnId + '" type="button" class="btn-sm" style="white-space:nowrap;"'
        + (isPulling ? ' disabled' : '')
        + ' onclick="pullSandboxImage(\'' + escapeHtml(sb) + '\')">' + btnLabel + '</button>'
        + deleteBtn;

      container.appendChild(row);

      // Progress area.
      var progressEl = document.createElement('pre');
      progressEl.id = progressId;
      progressEl.style.cssText = (isPulling ? 'display:block;' : 'display:none;')
        + 'margin:4px 0 0;padding:6px 8px;background:var(--bg-secondary,#1a1a1a);color:var(--text-primary,#e0e0e0);border-radius:4px;font-size:11px;max-height:120px;overflow-y:auto;white-space:pre-wrap;word-break:break-all;';
      // Restore accumulated log from active pull.
      if (isPulling && active.log) {
        progressEl.textContent = active.log;
      }
      container.appendChild(progressEl);

      // Reconnect SSE if a pull is active but the EventSource was lost
      // (e.g. settings modal was closed and DOM elements were recreated).
      if (active && active.pullId && (!active.source || active.source.readyState === EventSource.CLOSED)) {
        // The pull might still be running server-side. Re-stream to pick up
        // remaining events. If it already finished, the SSE will emit
        // done/error immediately.
        _connectPullStream(active.pullId, sb, progressEl, document.getElementById(btnId));
      }
    });
  } catch (e) {
    container.innerHTML = '<div style="font-size:12px; color:var(--text-error,red);">Failed to load image status.</div>';
    console.error('loadImageStatus:', e);
  }
}

// Deletes a cached sandbox image.
async function deleteSandboxImage(sandboxType) {
  if (!confirm('Remove the ' + sandboxType + ' sandbox image? You can re-pull it later.')) return;
  try {
    await api(Routes.images.remove(), {
      method: 'DELETE',
      body: JSON.stringify({ sandbox: sandboxType }),
    });
    loadImageStatus();
  } catch (e) {
    showAlert('Failed to remove image: ' + e.message);
  }
}

// Starts an image pull for the given sandbox type.
async function pullSandboxImage(sandboxType) {
  var btn = document.getElementById('pull-btn-' + sandboxType);
  var progressEl = document.getElementById('pull-progress-' + sandboxType);
  if (!btn || !progressEl) return;

  btn.disabled = true;
  btn.textContent = 'Pulling\u2026';
  progressEl.style.display = 'block';
  progressEl.textContent = '';

  try {
    var resp = await api(Routes.images.pull(), {
      method: 'POST',
      body: JSON.stringify({ sandbox: sandboxType }),
    });
    var pullId = resp.pull_id;
    if (!pullId) throw new Error('No pull_id returned');

    _activePulls[sandboxType] = { pullId: pullId, log: '', source: null };
    _connectPullStream(pullId, sandboxType, progressEl, btn);
  } catch (e) {
    progressEl.textContent = 'Error: ' + e.message;
    btn.disabled = false;
    btn.textContent = 'Retry';
  }
}

// Connects to the SSE stream for pull progress.
function _connectPullStream(pullId, sandboxType, progressEl, btn) {
  var url = Routes.images.pullStream() + '?pull_id=' + encodeURIComponent(pullId);
  var source = new EventSource(url);

  if (_activePulls[sandboxType]) {
    _activePulls[sandboxType].source = source;
  }

  source.addEventListener('progress', function(e) {
    try {
      var data = JSON.parse(e.data);
      var line = data.line + '\n';
      if (_activePulls[sandboxType]) _activePulls[sandboxType].log += line;
      // progressEl may have been removed if settings closed and reopened;
      // write to the current DOM element by ID.
      var el = document.getElementById('pull-progress-' + sandboxType);
      if (el) {
        el.textContent += line;
        el.scrollTop = el.scrollHeight;
      }
    } catch (_) {}
  });

  source.addEventListener('done', function() {
    source.close();
    delete _activePulls[sandboxType];
    var el = document.getElementById('pull-progress-' + sandboxType);
    if (el) el.textContent += '\nPull complete.\n';
    var b = document.getElementById('pull-btn-' + sandboxType);
    if (b) { b.disabled = false; b.textContent = 'Re-pull'; }
    // Refresh to show updated cached status.
    loadImageStatus();
  });

  source.addEventListener('error', function(e) {
    source.close();
    delete _activePulls[sandboxType];
    var el = document.getElementById('pull-progress-' + sandboxType);
    if (el) {
      if (e.data) {
        try { el.textContent += '\nError: ' + JSON.parse(e.data).error + '\n'; }
        catch (_) { el.textContent += '\nPull failed.\n'; }
      } else {
        el.textContent += '\nPull failed.\n';
      }
    }
    var b = document.getElementById('pull-btn-' + sandboxType);
    if (b) { b.disabled = false; b.textContent = 'Retry'; }
  });

  source.onerror = function() {
    if (source.readyState === EventSource.CLOSED) return;
    source.close();
    delete _activePulls[sandboxType];
    var el = document.getElementById('pull-progress-' + sandboxType);
    if (el) el.textContent += '\nConnection lost.\n';
    var b = document.getElementById('pull-btn-' + sandboxType);
    if (b) { b.disabled = false; b.textContent = 'Retry'; }
  };
}
