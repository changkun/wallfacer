// Parse a raw unified git diff (as returned by GET /api/tasks/{id}/diff) into a
// render-friendly per-file structure. Ports ui/js/modal-diff.js's parseDiffByFile
// + line classification into typed data the Vue template renders directly (no
// innerHTML). Syntax highlighting is intentionally left to the CSS layer for now.

export type DiffLineKind = 'add' | 'del' | 'hunk' | 'header' | 'ctx';

export interface DiffLine {
  kind: DiffLineKind;
  text: string;
  // Old/new file line numbers derived from the hunk headers, for anchoring
  // inline comments and rendering meaningful line references. `add` carries only
  // newLine, `del` only oldLine, `ctx` both; `hunk`/`header` carry neither.
  oldLine: number | null;
  newLine: number | null;
}

export interface DiffFile {
  filename: string;
  workspace: string;
  adds: number;
  dels: number;
  lines: DiffLine[];
}

const WORKSPACE_RE = /^=== (.+) ===$/;
const HUNK_RE = /^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/;

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
  // applyWorkspaceSeparators advances currentWorkspace for any "=== name ==="
  // line in the block. The server emits "=== name ===\n<diff>" with no trailing
  // separator, so after the split a separator lands at the TAIL of the previous
  // file's block — it belongs to the NEXT file, not the current one. We
  // therefore apply separators only AFTER pushing the current block's file.
  const applyWorkspaceSeparators = (rawLines: string[]) => {
    for (const line of rawLines) {
      const ws = line.match(WORKSPACE_RE);
      if (ws) currentWorkspace = ws[1];
    }
  };
  for (const block of blocks) {
    if (!block.trim()) continue;
    const rawLines = block.split('\n');
    const header = rawLines[0].match(/^diff --git a\/.+ b\/(.+)$/);
    if (!header) {
      // Bare separator block (e.g. the leading "=== name ===" before the first
      // file): set the workspace for the following files.
      applyWorkspaceSeparators(rawLines);
      continue;
    }
    const filename = header[1];
    let adds = 0;
    let dels = 0;
    // Old/new line counters, seeded by each hunk header and advanced per line.
    let oldNo = 0;
    let newNo = 0;
    const lines: DiffLine[] = [];
    for (const line of rawLines) {
      if (WORKSPACE_RE.test(line)) continue; // drop workspace separators from the body
      const kind = classifyDiffLine(line);
      let oldLine: number | null = null;
      let newLine: number | null = null;
      if (kind === 'hunk') {
        const m = line.match(HUNK_RE);
        if (m) {
          oldNo = parseInt(m[1], 10);
          newNo = parseInt(m[2], 10);
        }
      } else if (kind === 'add') {
        adds++;
        newLine = newNo++;
      } else if (kind === 'del') {
        dels++;
        oldLine = oldNo++;
      } else if (kind === 'ctx') {
        oldLine = oldNo++;
        newLine = newNo++;
      }
      lines.push({ kind, text: line, oldLine, newLine });
    }
    files.push({ filename, workspace: currentWorkspace, adds, dels, lines });
    // A separator at the tail of this block introduces the next workspace.
    applyWorkspaceSeparators(rawLines);
  }
  return files;
}
