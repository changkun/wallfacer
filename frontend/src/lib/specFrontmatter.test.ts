import { describe, it, expect } from 'vitest';
import { parseSpecFrontmatter } from './specFrontmatter';

describe('parseSpecFrontmatter', () => {
  it('returns empty fm + body for empty input', () => {
    expect(parseSpecFrontmatter('')).toEqual({ frontmatter: {}, body: '' });
  });
  it('treats markdown without frontmatter as a body-only spec', () => {
    const r = parseSpecFrontmatter('# Hello\n\nNo fm here.');
    expect(r.frontmatter).toEqual({});
    expect(r.body).toBe('# Hello\n\nNo fm here.');
    expect(r.warning).toBeUndefined();
  });
  it('parses a well-formed frontmatter block', () => {
    const r = parseSpecFrontmatter('---\ntitle: My Spec\nstatus: drafted\n---\nbody here');
    expect(r.frontmatter.title).toBe('My Spec');
    expect(r.frontmatter.status).toBe('drafted');
    expect(r.body).toBe('body here');
  });
  it('warns when a leading --- has no closing fence', () => {
    const r = parseSpecFrontmatter('---\ntitle: oops\n# never closed');
    expect(r.frontmatter).toEqual({});
    expect(r.warning).toContain('closing `---`');
  });
  it('skips list, block-scalar, and colon-less lines silently', () => {
    const r = parseSpecFrontmatter('---\ndepends_on:\n  - a\n  - b\ndescription: |\nbroken line\ntitle: Kept\n---\nbody');
    expect(r.frontmatter.title).toBe('Kept');
    expect(r.frontmatter.depends_on).toBeUndefined();
    expect(r.frontmatter.description).toBeUndefined();
  });
  // Body line 1 must be the first content line, matching the backend's
  // spec.ParseBytes TrimLeft. Almost every spec has a blank line after the
  // closing ---; if it were kept, data-source-line would offset every spec
  // comment by one line. Guard the alignment.
  it('strips leading blank lines so body line 1 is the first heading', () => {
    const r = parseSpecFrontmatter('---\ntitle: X\n---\n\n# Heading\n\nFirst paragraph.\n');
    expect(r.body.startsWith('# Heading')).toBe(true);
    expect(r.body.split('\n')[0]).toBe('# Heading');
  });
});
