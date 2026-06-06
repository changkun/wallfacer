import { describe, it, expect, vi, afterEach } from 'vitest';
import { openMermaidOverlay } from './useMermaid';

function makeSource(): HTMLElement {
  const src = document.createElement('div');
  src.innerHTML = '<svg><rect/></svg>';
  document.body.appendChild(src);
  return src;
}

describe('openMermaidOverlay', () => {
  afterEach(() => {
    document.body.innerHTML = '';
    vi.restoreAllMocks();
  });

  it('removes the document keydown listener when closed by backdrop click', () => {
    const addSpy = vi.spyOn(document, 'addEventListener');
    const removeSpy = vi.spyOn(document, 'removeEventListener');

    openMermaidOverlay(makeSource());
    const overlay = document.querySelector('.mermaid-overlay') as HTMLElement;
    expect(overlay).toBeTruthy();
    expect(addSpy.mock.calls.filter(([t]) => t === 'keydown')).toHaveLength(1);

    // Click the backdrop (target === overlay) to close.
    overlay.dispatchEvent(new MouseEvent('click', { bubbles: true }));

    expect(document.querySelector('.mermaid-overlay')).toBeNull();
    // The keydown listener must have been removed on this close path too.
    expect(removeSpy.mock.calls.filter(([t]) => t === 'keydown')).toHaveLength(1);
  });

  it('removes the keydown listener when closed by Escape', () => {
    const removeSpy = vi.spyOn(document, 'removeEventListener');
    openMermaidOverlay(makeSource());
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    expect(document.querySelector('.mermaid-overlay')).toBeNull();
    expect(removeSpy.mock.calls.filter(([t]) => t === 'keydown')).toHaveLength(1);
  });
});
