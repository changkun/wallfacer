// --- Diff helpers ---

function parseDiffByFile(diff) {
  const files = [];
  // Track current workspace section from "=== name ===" separators.
  let currentWorkspace = '';
  const blocks = diff.split(/(?=^diff --git )/m);
  for (const block of blocks) {
    if (!block.trim()) continue;
    const lines = block.split('\n');
    // Extract workspace separator if present (before or after diff content).
    for (const line of lines) {
      const wsMatch = line.match(/^=== (.+) ===$/);
      if (wsMatch) currentWorkspace = wsMatch[1];
    }
    const match = lines[0].match(/^diff --git a\/.+ b\/(.+)$/);
    if (!match) continue; // skip blocks without diff header (e.g. bare separators)
    const filename = match[1];
    let adds = 0, dels = 0;
    for (const line of lines.slice(1)) {
      if (line.startsWith('+') && !line.startsWith('+++')) adds++;
      if (line.startsWith('-') && !line.startsWith('---')) dels++;
    }
    files.push({ filename, content: block, adds, dels, workspace: currentWorkspace });
  }
  return files;
}

function renderDiffLine(line) {
  const escaped = escapeHtml(line);
  if (line.startsWith('+') && !line.startsWith('+++')) return `<span class="diff-line diff-add">${escaped}</span>`;
  if (line.startsWith('-') && !line.startsWith('---')) return `<span class="diff-line diff-del">${escaped}</span>`;
  if (line.startsWith('@@')) return `<span class="diff-line diff-hunk">${escaped}</span>`;
  if (/^(diff |--- |\+{3} |index |Binary )/.test(line)) return `<span class="diff-line diff-header">${escaped}</span>`;
  return `<span class="diff-line">${escaped}</span>`;
}

function renderDiffFiles(container, diff) {
  if (!diff) {
    container.innerHTML = '<span class="text-xs text-v-muted">No changes</span>';
    return;
  }
  const files = parseDiffByFile(diff);
  if (files.length === 0) {
    container.innerHTML = '<span class="text-xs text-v-muted">No changes</span>';
    return;
  }
  let lastWorkspace = '';
  container.innerHTML = files.map(f => {
    let wsHeader = '';
    if (f.workspace && f.workspace !== lastWorkspace) {
      lastWorkspace = f.workspace;
      wsHeader = `<div class="diff-workspace-header">${escapeHtml(f.workspace)}</div>`;
    }
    const statsHtml = [
      f.adds > 0 ? `<span class="diff-add">+${f.adds}</span>` : '',
      f.dels > 0 ? `<span class="diff-del">&minus;${f.dels}</span>` : '',
    ].filter(Boolean).join(' ');
    const diffHtml = f.content.split('\n').filter(l => !/^=== .+ ===$/.test(l)).map(renderDiffLine).join('\n');
    return wsHeader + `<details class="diff-file">
      <summary class="diff-file-summary">
        <span class="diff-filename">${escapeHtml(f.filename)}</span>
        <span class="diff-stats">${statsHtml}</span>
      </summary>
      <pre class="diff-block diff-block-modal">${diffHtml}</pre>
    </details>`;
  }).join('');
}
