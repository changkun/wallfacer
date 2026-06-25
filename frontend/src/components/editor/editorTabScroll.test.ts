// The watcher in EditorTabStrip.vue scrolls the active tab into view when the
// active id changes. Opening a file from the Explorer never focuses the tab
// button, so without this the newest tab is left sliced past the strip's right
// edge (verified in a real browser: active tab at x=1067 with strip ending at
// 654). These pin the wiring contract: the right tab is targeted with the
// minimal-scroll options.
import { describe, it, expect, vi } from 'vitest';
import { scrollActiveTabIntoView } from './editorTabScroll';

describe('scrollActiveTabIntoView', () => {
  it('scrolls the active tab into view with nearest (minimal-jump) options', () => {
    const active = { scrollIntoView: vi.fn() };
    const strip = { querySelector: vi.fn(() => active) } as unknown as HTMLElement;
    scrollActiveTabIntoView(strip);
    expect((strip.querySelector as unknown as ReturnType<typeof vi.fn>)).toHaveBeenCalledWith(
      '.editor-tab--active',
    );
    expect(active.scrollIntoView).toHaveBeenCalledWith({ inline: 'nearest', block: 'nearest' });
  });

  it('no-ops when the strip ref is null', () => {
    expect(() => scrollActiveTabIntoView(null)).not.toThrow();
  });

  it('no-ops when there is no active tab', () => {
    const strip = { querySelector: vi.fn(() => null) } as unknown as HTMLElement;
    expect(() => scrollActiveTabIntoView(strip)).not.toThrow();
  });
});
