import { describe, it, expect } from 'vitest';
import { fileIcon, PATHS } from './fileIcon';

describe('fileIcon', () => {
  it('uses folder/folder-open glyphs for directories', () => {
    expect(fileIcon('src', true, false).paths).toBe(PATHS.folder);
    expect(fileIcon('src', true, true).paths).toBe(PATHS.folderOpen);
  });
  it('colours by extension', () => {
    expect(fileIcon('main.go', false).color).toBe('#00ADD8');
    expect(fileIcon('App.tsx', false).color).toBe('#3178C6');
  });
  it('uses database glyph for .sql', () => {
    expect(fileIcon('schema.sql', false).paths).toBe(PATHS.database);
  });
  it('uses image glyph for images', () => {
    expect(fileIcon('logo.PNG', false).paths).toBe(PATHS.image);
  });
  it('uses gear glyph for config files', () => {
    expect(fileIcon('.env', false).paths).toBe(PATHS.gear);
    expect(fileIcon('config.toml', false).paths).toBe(PATHS.gear);
  });
  it('matches special filenames case-insensitively', () => {
    expect(fileIcon('Makefile', false).color).toBe('#6D8C2E');
    expect(fileIcon('Dockerfile', false).color).toBe('#2496ED');
    expect(fileIcon('AGENTS.md', false).color).toBe('#D97757');
  });
  it('matches dockerfile/compose and dotfile patterns', () => {
    expect(fileIcon('docker-compose.yml', false).color).toBe('#2496ED');
    expect(fileIcon('.gitignore', false).color).toBe('#E44D26');
    expect(fileIcon('README.md', false).color).toBe('#6CB6FF');
  });
  it('falls back to a muted plain file icon', () => {
    const ic = fileIcon('mystery.xyz', false);
    expect(ic.paths).toBe(PATHS.file);
    expect(ic.color).toBe('var(--text-muted)');
  });
});
