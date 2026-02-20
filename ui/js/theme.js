// --- Theme management ---

function getResolvedTheme(mode) {
  if (mode === 'auto') return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  return mode;
}

function setTheme(mode) {
  localStorage.setItem('wallfacer-theme', mode);
  document.documentElement.setAttribute('data-theme', getResolvedTheme(mode));
  document.querySelectorAll('#theme-switch button').forEach(function(btn) {
    btn.classList.toggle('active', btn.dataset.mode === mode);
  });
}

// Mark the active theme button on load
(function() {
  var mode = localStorage.getItem('wallfacer-theme') || 'auto';
  document.querySelectorAll('#theme-switch button').forEach(function(btn) {
    btn.classList.toggle('active', btn.dataset.mode === mode);
  });
})();

// Re-apply theme when OS preference changes
window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', function() {
  var mode = localStorage.getItem('wallfacer-theme') || 'auto';
  if (mode === 'auto') document.documentElement.setAttribute('data-theme', getResolvedTheme('auto'));
});

// --- Settings panel ---

function toggleSettings(e) {
  e.stopPropagation();
  document.getElementById('settings-panel').classList.toggle('hidden');
}

// Close settings panel on outside click
document.addEventListener('click', function(e) {
  var panel = document.getElementById('settings-panel');
  var btn = document.getElementById('settings-btn');
  if (!panel.contains(e.target) && !btn.contains(e.target)) {
    panel.classList.add('hidden');
  }
});
