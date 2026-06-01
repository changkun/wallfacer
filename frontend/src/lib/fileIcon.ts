// Semantic file-type icons for the explorer tree, ported 1:1 from the legacy
// ui/js/explorer.js icon map. Returns a stroke colour + the inner SVG path
// fragments; the component wraps them in a <svg viewBox="0 0 24 24">.

const MUTED = 'var(--text-muted)';

// Standard path fragments (Feather-style 24x24 icons).
export const PATHS = {
  folder:
    '<path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path>',
  folderOpen:
    '<path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path><line x1="9" y1="9" x2="9" y2="21"></line>',
  file:
    '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><polyline points="14 2 14 8 20 8"></polyline>',
  gear:
    '<circle cx="12" cy="12" r="3"></circle><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09a1.65 1.65 0 0 0-1.08-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09a1.65 1.65 0 0 0 1.51-1.08 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"></path>',
  image:
    '<rect x="3" y="3" width="18" height="18" rx="2" ry="2"></rect><circle cx="8.5" cy="8.5" r="1.5"></circle><polyline points="21 15 16 10 5 21"></polyline>',
  database:
    '<ellipse cx="12" cy="5" rx="9" ry="3"></ellipse><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"></path><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"></path>',
};

export interface FileIcon {
  color: string;
  paths: string;
}

// Extension → {color, glyph} map.
const EXT: Record<string, FileIcon> = {
  go: { color: '#00ADD8', paths: PATHS.file },
  js: { color: '#F0DB4F', paths: PATHS.file },
  mjs: { color: '#F0DB4F', paths: PATHS.file },
  cjs: { color: '#F0DB4F', paths: PATHS.file },
  jsx: { color: '#61DAFB', paths: PATHS.file },
  ts: { color: '#3178C6', paths: PATHS.file },
  tsx: { color: '#3178C6', paths: PATHS.file },
  css: { color: '#A86EDB', paths: PATHS.file },
  scss: { color: '#C76494', paths: PATHS.file },
  html: { color: '#E44D26', paths: PATHS.file },
  htm: { color: '#E44D26', paths: PATHS.file },
  json: { color: '#A0B840', paths: PATHS.file },
  md: { color: '#6CB6FF', paths: PATHS.file },
  mdx: { color: '#6CB6FF', paths: PATHS.file },
  yaml: { color: '#CB4B60', paths: PATHS.file },
  yml: { color: '#CB4B60', paths: PATHS.file },
  py: { color: '#3776AB', paths: PATHS.file },
  pyi: { color: '#3776AB', paths: PATHS.file },
  rs: { color: '#C4623F', paths: PATHS.file },
  sh: { color: '#4EAA25', paths: PATHS.file },
  bash: { color: '#4EAA25', paths: PATHS.file },
  zsh: { color: '#4EAA25', paths: PATHS.file },
  sql: { color: '#E8A838', paths: PATHS.database },
  env: { color: MUTED, paths: PATHS.gear },
  toml: { color: MUTED, paths: PATHS.gear },
  ini: { color: MUTED, paths: PATHS.gear },
  cfg: { color: MUTED, paths: PATHS.gear },
  png: { color: '#4EAA86', paths: PATHS.image },
  jpg: { color: '#4EAA86', paths: PATHS.image },
  jpeg: { color: '#4EAA86', paths: PATHS.image },
  gif: { color: '#C060C0', paths: PATHS.image },
  svg: { color: '#E44D26', paths: PATHS.image },
  webp: { color: '#4EAA86', paths: PATHS.image },
  ico: { color: '#4EAA86', paths: PATHS.image },
  txt: { color: MUTED, paths: PATHS.file },
  log: { color: MUTED, paths: PATHS.file },
};

// Special full-filename matches (case-insensitive).
const NAME: Record<string, FileIcon> = {
  makefile: { color: '#6D8C2E', paths: PATHS.gear },
  dockerfile: { color: '#2496ED', paths: PATHS.file },
  license: { color: '#D4A520', paths: PATHS.file },
  'claude.md': { color: '#D97757', paths: PATHS.file },
  'agents.md': { color: '#D97757', paths: PATHS.file },
};

export function fileIcon(name: string, isDir: boolean, expanded = false): FileIcon {
  if (isDir) {
    return { color: MUTED, paths: expanded ? PATHS.folderOpen : PATHS.folder };
  }
  const lower = (name || '').toLowerCase();

  if (NAME[lower]) return NAME[lower];

  if (lower.indexOf('dockerfile') === 0 || lower.indexOf('docker-compose') === 0) {
    return { color: '#2496ED', paths: PATHS.file };
  }
  if (lower === '.gitignore' || lower === '.gitmodules' || lower === '.gitattributes') {
    return { color: '#E44D26', paths: PATHS.file };
  }
  if (lower.indexOf('readme') === 0) {
    return { color: '#6CB6FF', paths: PATHS.file };
  }

  const dot = lower.lastIndexOf('.');
  if (dot >= 0) {
    const ext = lower.slice(dot + 1);
    if (EXT[ext]) return EXT[ext];
  }
  return { color: MUTED, paths: PATHS.file };
}
