// --- Documentation viewer ---

var _docsDismiss = null;
var _docsEntries = [];
var _docsCurrentSlug = '';

async function openDocs(slug) {
  var modal = document.getElementById('docs-modal');
  if (!modal) return;
  modal.classList.remove('hidden');
  modal.style.display = 'flex';
  if (_docsDismiss) _docsDismiss();
  _docsDismiss = bindModalDismiss(modal, closeDocs);

  if (!_docsEntries.length) {
    try {
      _docsEntries = await api('/api/docs');
    } catch (e) {
      _docsEntries = [];
    }
  }
  renderDocsNav();
  var target = slug || (_docsEntries.length ? _docsEntries[0].slug : '');
  if (target) loadDoc(target);
}

function closeDocs() {
  var modal = document.getElementById('docs-modal');
  if (!modal) return;
  modal.classList.add('hidden');
  modal.style.display = '';
  if (_docsDismiss) { _docsDismiss(); _docsDismiss = null; }
}

function renderDocsNav() {
  var nav = document.getElementById('docs-nav');
  if (!nav) return;
  var categories = {};
  _docsEntries.forEach(function(entry) {
    if (!categories[entry.category]) categories[entry.category] = [];
    categories[entry.category].push(entry);
  });
  var html = '';
  var catLabels = { guide: 'User Guide', internals: 'Technical Reference' };
  Object.keys(categories).forEach(function(cat) {
    html += '<div style="margin-bottom:12px;">';
    html += '<div style="font-size:10px;font-weight:700;text-transform:uppercase;letter-spacing:0.05em;color:var(--text-muted);margin-bottom:6px;">' + escapeHtml(catLabels[cat] || cat) + '</div>';
    categories[cat].forEach(function(entry) {
      var active = entry.slug === _docsCurrentSlug;
      html += '<button type="button" onclick="loadDoc(\'' + escapeHtml(entry.slug) + '\')" style="display:block;width:100%;text-align:left;padding:4px 8px;margin-bottom:2px;border:none;border-radius:4px;background:' + (active ? 'var(--bg-input)' : 'transparent') + ';color:' + (active ? 'var(--text-primary)' : 'inherit') + ';font-size:12px;cursor:pointer;font-weight:' + (active ? '600' : '400') + ';" onmouseover="this.style.background=\'var(--bg-input)\'" onmouseout="this.style.background=\'' + (active ? 'var(--bg-input)' : 'transparent') + '\'">' + escapeHtml(entry.title) + '</button>';
    });
    html += '</div>';
  });
  nav.innerHTML = html;
}

async function loadDoc(slug) {
  _docsCurrentSlug = slug;
  renderDocsNav();
  var content = document.getElementById('docs-content');
  if (!content) return;
  content.innerHTML = '<div style="color:var(--text-muted);font-size:12px;">Loading...</div>';
  try {
    var res = await fetch(withAuthToken('/api/docs/' + slug));
    if (!res.ok) throw new Error('Not found');
    var md = await res.text();
    content.innerHTML = renderMarkdown(md);
  } catch (e) {
    content.innerHTML = '<div style="color:var(--text-muted);">Failed to load document.</div>';
  }
}
