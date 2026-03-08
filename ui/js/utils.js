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
