// Parse a raw unified git diff (as returned by GET /api/tasks/{id}/diff) into a
// render-friendly per-file structure. Ports ui/js/modal-diff.js's parseDiffByFile
// + line classification into typed data the Vue template renders directly (no
// innerHTML). Syntax highlighting is intentionally left to the CSS layer for now.

export type DiffLineKind = 'add' | 'del' | 'hunk' | 'header' | 'ctx';

export interface DiffLine {
  kind: DiffLineKind;
  text: string;
}

export interface DiffFile {
  filename: string;
  workspace: string;
  adds: number;
  dels: number;
  lines: DiffLine[];
}

const WORKSPACE_RE = /^=== (.+) ===$/;

export function classifyDiffLine(line: string): DiffLineKind {
  if (line.startsWith('+') && !line.startsWith('+++')) return 'add';
  if (line.startsWith('-') && !line.startsWith('---')) return 'del';
  if (line.startsWith('@@')) return 'hunk';
  if (/^(diff |--- |\+{3} |index |Binary |new file|deleted file|rename |similarity )/.test(line)) {
    return 'header';
  }
  return 'ctx';
}

export function parseDiffFiles(diff: string): DiffFile[] {
  if (!diff || !diff.trim()) return [];
  const files: DiffFile[] = [];
  let currentWorkspace = '';
  // Split before each "diff --git " header so each block is one file.
  const blocks = diff.split(/(?=^diff --git )/m);
  for (const block of blocks) {
    if (!block.trim()) continue;
    const rawLines = block.split('\n');
    // A "=== name ===" separator marks which workspace the following files belong to.
    for (const line of rawLines) {
      const ws = line.match(WORKSPACE_RE);
      if (ws) currentWorkspace = ws[1];
    }
    const header = rawLines[0].match(/^diff --git a\/.+ b\/(.+)$/);
    if (!header) continue; // bare separator block, no file
    const filename = header[1];
    let adds = 0;
    let dels = 0;
    const lines: DiffLine[] = [];
    for (const line of rawLines) {
      if (WORKSPACE_RE.test(line)) continue; // drop workspace separators from the body
      const kind = classifyDiffLine(line);
      if (kind === 'add') adds++;
      else if (kind === 'del') dels++;
      lines.push({ kind, text: line });
    }
    files.push({ filename, workspace: currentWorkspace, adds, dels, lines });
  }
  return files;
}
