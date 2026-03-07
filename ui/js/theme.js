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

// --- Settings modal ---

function openSettings() {
  var modal = document.getElementById('settings-modal');
  modal.classList.remove('hidden');
  modal.style.display = 'flex';
}

function closeSettings() {
  var modal = document.getElementById('settings-modal');
  if (!modal) return;
  modal.classList.add('hidden');
  modal.style.display = '';
}
