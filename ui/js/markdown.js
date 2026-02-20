// --- Markdown helpers ---

function renderMarkdown(text) {
  if (!text) return '';
  if (typeof marked === 'undefined') return escapeHtml(text);
  return marked.parse(text);
}

function renderMarkdownInline(text) {
  if (!text) return '';
  if (typeof marked === 'undefined') return escapeHtml(text);
  return marked.parseInline(text);
}

function toggleModalSection(section) {
  const renderedEl = document.getElementById('modal-' + section + '-rendered');
  const rawEl = document.getElementById('modal-' + section);
  const btn = document.getElementById('toggle-' + section + '-btn');
  const showingRaw = !rawEl.classList.contains('hidden');
  if (showingRaw) {
    renderedEl.classList.remove('hidden');
    rawEl.classList.add('hidden');
    btn.textContent = 'Raw';
  } else {
    renderedEl.classList.add('hidden');
    rawEl.classList.remove('hidden');
    btn.textContent = 'Preview';
  }
}

function copyModalText(section) {
  const rawEl = document.getElementById('modal-' + section);
  const text = rawEl.textContent;
  const btn = document.getElementById('copy-' + section + '-btn');
  navigator.clipboard.writeText(text).then(function() {
    const origHTML = btn.innerHTML;
    btn.textContent = 'Copied!';
    setTimeout(function() { btn.innerHTML = origHTML; }, 1500);
  }).catch(function() {});
}

function toggleCardMarkdown(event, btn) {
  event.stopPropagation();
  const card = btn.closest('.card');
  const renderedEls = card.querySelectorAll('.card-md-rendered');
  const rawEls = card.querySelectorAll('.card-md-raw');
  const nowShowingRaw = card.dataset.rawView === 'true';
  if (nowShowingRaw) {
    card.dataset.rawView = 'false';
    renderedEls.forEach(function(el) { el.classList.remove('hidden'); });
    rawEls.forEach(function(el) { el.classList.add('hidden'); });
    btn.textContent = 'Raw';
  } else {
    card.dataset.rawView = 'true';
    renderedEls.forEach(function(el) { el.classList.add('hidden'); });
    rawEls.forEach(function(el) { el.classList.remove('hidden'); });
    btn.textContent = 'Preview';
  }
}

function copyCardText(event, taskId) {
  event.stopPropagation();
  const task = tasks.find(function(t) { return t.id === taskId; });
  if (!task) return;
  const text = task.prompt + (task.result ? '\n\n' + task.result : '');
  const btn = event.currentTarget;
  navigator.clipboard.writeText(text).then(function() {
    const origHTML = btn.innerHTML;
    btn.textContent = '\u2713';
    setTimeout(function() { btn.innerHTML = origHTML; }, 1500);
  }).catch(function() {});
}
