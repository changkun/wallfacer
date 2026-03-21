// --- Sandbox Image Management ---

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
      var row = document.createElement('div');
      row.style.cssText = 'display:flex; align-items:center; gap:8px; padding:8px 10px; border:1px solid var(--border); border-radius:6px; font-size:12px;';

      var badge = img.cached
        ? '<span style="display:inline-block;padding:1px 6px;border-radius:4px;background:#dcfce7;color:#166534;font-size:11px;font-weight:600;">Cached</span>'
        : '<span style="display:inline-block;padding:1px 6px;border-radius:4px;background:#fef2f2;color:#991b1b;font-size:11px;font-weight:600;">Missing</span>';

      var info = '<div style="flex:1;min-width:0;">'
        + '<div style="font-weight:600;text-transform:capitalize;">' + escapeHtml(String(img.sandbox)) + '</div>'
        + '<div style="font-size:11px;color:var(--text-muted);overflow:hidden;text-overflow:ellipsis;white-space:nowrap;" title="' + escapeHtml(img.image) + '">' + escapeHtml(img.image) + '</div>'
        + (img.size ? '<div style="font-size:11px;color:var(--text-muted);">Size: ' + escapeHtml(img.size) + '</div>' : '')
        + '</div>';

      var btnLabel = img.cached ? 'Re-pull' : 'Pull';
      var btnId = 'pull-btn-' + img.sandbox;
      var progressId = 'pull-progress-' + img.sandbox;

      row.innerHTML = info + badge
        + ' <button id="' + btnId + '" type="button" class="btn-sm" style="white-space:nowrap;" onclick="pullSandboxImage(\'' + escapeHtml(String(img.sandbox)) + '\')">' + btnLabel + '</button>';

      container.appendChild(row);

      // Progress area (hidden until pull starts).
      var progressEl = document.createElement('pre');
      progressEl.id = progressId;
      progressEl.style.cssText = 'display:none;margin:4px 0 0;padding:6px 8px;background:var(--bg-secondary,#1a1a1a);color:var(--text-primary,#e0e0e0);border-radius:4px;font-size:11px;max-height:120px;overflow-y:auto;white-space:pre-wrap;word-break:break-all;';
      container.appendChild(progressEl);
    });
  } catch (e) {
    container.innerHTML = '<div style="font-size:12px; color:var(--text-error,red);">Failed to load image status.</div>';
    console.error('loadImageStatus:', e);
  }
}

// Starts an image pull for the given sandbox type.
async function pullSandboxImage(sandboxType) {
  var btn = document.getElementById('pull-btn-' + sandboxType);
  var progressEl = document.getElementById('pull-progress-' + sandboxType);
  if (!btn || !progressEl) return;

  btn.disabled = true;
  btn.textContent = 'Pulling...';
  progressEl.style.display = 'block';
  progressEl.textContent = '';

  try {
    var resp = await api(Routes.images.pull(), {
      method: 'POST',
      body: JSON.stringify({ sandbox: sandboxType }),
    });
    var pullId = resp.pull_id;
    if (!pullId) throw new Error('No pull_id returned');

    streamPullProgress(pullId, sandboxType, progressEl, btn);
  } catch (e) {
    progressEl.textContent = 'Error: ' + e.message;
    btn.disabled = false;
    btn.textContent = 'Retry';
  }
}

// Connects to the SSE stream for pull progress.
function streamPullProgress(pullId, sandboxType, progressEl, btn) {
  var url = Routes.images.pullStream() + '?pull_id=' + encodeURIComponent(pullId);
  var source = new EventSource(url);

  source.addEventListener('progress', function(e) {
    try {
      var data = JSON.parse(e.data);
      progressEl.textContent += data.line + '\n';
      progressEl.scrollTop = progressEl.scrollHeight;
    } catch (_) {}
  });

  source.addEventListener('done', function(e) {
    source.close();
    btn.disabled = false;
    btn.textContent = 'Re-pull';
    progressEl.textContent += '\nPull complete.\n';
    // Refresh the image status display.
    loadImageStatus();
  });

  source.addEventListener('error', function(e) {
    if (e.data) {
      try {
        var data = JSON.parse(e.data);
        progressEl.textContent += '\nError: ' + data.error + '\n';
      } catch (_) {
        progressEl.textContent += '\nPull failed.\n';
      }
    } else {
      progressEl.textContent += '\nConnection lost.\n';
    }
    source.close();
    btn.disabled = false;
    btn.textContent = 'Retry';
  });

  source.onerror = function() {
    if (source.readyState === EventSource.CLOSED) return;
    source.close();
    btn.disabled = false;
    btn.textContent = 'Retry';
    progressEl.textContent += '\nConnection lost.\n';
  };
}
