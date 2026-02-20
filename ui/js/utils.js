// --- Utility helpers ---

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
