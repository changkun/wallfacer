// --- Utility helpers ---

function showAlert(message) {
  document.getElementById('alert-message').textContent = message;
  const modal = document.getElementById('alert-modal');
  modal.classList.remove('hidden');
  modal.classList.add('flex');
  document.getElementById('alert-ok-btn').focus();
}

function closeAlert() {
  const modal = document.getElementById('alert-modal');
  modal.classList.add('hidden');
  modal.classList.remove('flex');
}

function escapeHtml(s) {
  if (!s) return '';
  return s.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
}

function createModalStateController(nodes) {
  var loadingEl = nodes && nodes.loadingEl;
  var errorEl = nodes && nodes.errorEl;
  var emptyEl = nodes && nodes.emptyEl;
  var contentEl = nodes && nodes.contentEl;
  var contentState = (nodes && nodes.contentState) || 'content';

  return function setModalState(state, msg) {
    if (loadingEl) loadingEl.style.display = state === 'loading' ? 'flex' : 'none';
    if (errorEl) errorEl.classList.toggle('hidden', state !== 'error');
    if (emptyEl) emptyEl.classList.toggle('hidden', state !== 'empty');
    if (contentEl) contentEl.classList.toggle('hidden', state !== contentState);
    if (state === 'error' && errorEl) errorEl.textContent = msg || 'Unknown error';
  };
}

function openModalPanel(modal) {
  if (!modal) return;
  modal.classList.remove('hidden');
  modal.style.display = 'flex';
}

function closeModalPanel(modal) {
  if (!modal) return;
  modal.classList.add('hidden');
  modal.style.display = '';
}

function bindModalBackdropClose(modal, onClose) {
  if (!modal || typeof onClose !== 'function') return;
  modal.addEventListener('click', function (e) {
    if (e.target === modal) onClose();
  });
}

function loadJsonEndpoint(url, onSuccess, setState, options) {
  setState('loading');
  return fetch(url, options || {})
    .then(function (res) {
      return res.json().then(function (data) { return { ok: res.ok, data: data }; });
    })
    .then(function (result) {
      if (!result.ok) {
        setState('error', result.data.error || JSON.stringify(result.data));
        return;
      }
      onSuccess(result.data);
    })
    .catch(function (err) {
      setState('error', String(err));
    });
}

function createHoverRow(cells) {
  var tr = document.createElement('tr');
  tr.style.cssText = 'border-bottom: 1px solid var(--border); transition: background 0.1s;';
  tr.addEventListener('mouseenter', function () { tr.style.background = 'var(--bg-raised)'; });
  tr.addEventListener('mouseleave', function () { tr.style.background = ''; });

  cells.forEach(function (cell) {
    var td = document.createElement('td');
    td.style.cssText = cell.style || 'padding:6px 10px;';
    if (cell.html != null) {
      td.innerHTML = cell.html;
    } else {
      td.textContent = cell.text != null ? cell.text : '';
    }
    tr.appendChild(td);
  });
  return tr;
}

function appendRowsToTbody(tbodyOrId, rows) {
  var tbody = typeof tbodyOrId === 'string' ? document.getElementById(tbodyOrId) : tbodyOrId;
  if (!tbody) return;
  tbody.innerHTML = '';
  (rows || []).forEach(function (row) {
    tbody.appendChild(createHoverRow(row));
  });
}

function appendNoDataRow(tbodyOrId, colSpan, message) {
  var tbody = typeof tbodyOrId === 'string' ? document.getElementById(tbodyOrId) : tbodyOrId;
  if (!tbody) return;
  var emptyRow = document.createElement('tr');
  emptyRow.innerHTML = '<td colspan="' + colSpan + '" style="padding:12px 10px;text-align:center;color:var(--text-muted);font-size:12px;">' + escapeHtml(message || 'No data') + '</td>';
  tbody.appendChild(emptyRow);
}

function closeFirstVisibleModal(modals) {
  for (var i = 0; i < modals.length; i++) {
    var item = modals[i];
    if (!item) continue;
    var modal = document.getElementById(item.id);
    if (!modal || modal.classList.contains('hidden')) continue;
    if (typeof item.close === 'function') item.close();
    return true;
  }
  return false;
}

function timeAgo(dateStr) {
  const d = new Date(dateStr);
  const s = Math.floor((Date.now() - d) / 1000);
  if (s < 60) return 'just now';
  if (s < 3600) return Math.floor(s / 60) + 'm ago';
  if (s < 86400) return Math.floor(s / 3600) + 'h ago';
  return Math.floor(s / 86400) + 'd ago';
}

function formatTimeout(minutes) {
  if (!minutes) return '5m';
  if (minutes < 60) return minutes + 'm';
  if (minutes % 60 === 0) return (minutes / 60) + 'h';
  return Math.floor(minutes / 60) + 'h' + (minutes % 60) + 'm';
}

// taskDisplayPrompt returns the prompt text that should be shown to users.
// For brainstorm runner cards we show the generated execution prompt once it
// exists so the card/modal reflects the actual synthesized instructions.
function taskDisplayPrompt(task) {
  if (task && task.kind === 'idea-agent' && task.execution_prompt) return task.execution_prompt;
  return task ? task.prompt : '';
}

function getTaskAccessibleTitle(task) {
  if (!task) return '';
  if (task.title) return task.title;
  if (task.prompt) return task.prompt.length > 60 ? task.prompt.slice(0, 60) + '\u2026' : task.prompt;
  return task.id || '';
}

function formatTaskStatusLabel(status) {
  return String(status || '').replace(/_/g, ' ');
}

function announceBoardStatus(message) {
  var announcer = document.getElementById('board-announcer');
  if (!announcer) return;
  announcer.textContent = '';
  announcer.textContent = message || '';
}

// --- Mobile column navigation ---

function scrollToColumn(wrapperId) {
  const el = document.getElementById(wrapperId);
  if (!el) return;
  el.scrollIntoView({ behavior: 'smooth', block: 'nearest', inline: 'start' });
}

// Keep the mobile nav active pill in sync with the visible column.
(function initMobileColNav() {
  function setup() {
    const board = document.getElementById('board');
    const nav = document.getElementById('mobile-col-nav');
    if (!board || !nav) return;

    const colWrapperIds = [
      'col-wrapper-backlog',
      'col-wrapper-in_progress',
      'col-wrapper-waiting',
      'col-wrapper-done',
      'col-wrapper-cancelled',
    ];

    const observer = new IntersectionObserver(function(entries) {
      entries.forEach(function(entry) {
        if (!entry.isIntersecting) return;
        const id = entry.target.id;
        nav.querySelectorAll('.mobile-col-btn').forEach(function(btn) {
          btn.classList.toggle('active', btn.dataset.col === id);
        });
      });
    }, {
      root: board,
      threshold: 0.5,
    });

    colWrapperIds.forEach(function(id) {
      const el = document.getElementById(id);
      if (el) observer.observe(el);
    });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', setup);
  } else {
    setup();
  }
})();
