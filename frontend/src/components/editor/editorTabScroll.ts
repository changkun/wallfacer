// Keep the active editor tab fully visible. The strip scrolls horizontally, so
// opening or focusing a tab past the right edge would leave it sliced (the tab
// button never receives DOM focus when a file is opened from the Explorer, so
// the browser's native focus-scroll does not kick in). Nudge the strip the
// minimum amount to reveal the active tab; `nearest` is a no-op when it is
// already visible. Extracted from EditorTabStrip.vue so the wiring is testable.
export function scrollActiveTabIntoView(strip: HTMLElement | null): void {
  strip
    ?.querySelector<HTMLElement>('.editor-tab--active')
    ?.scrollIntoView({ inline: 'nearest', block: 'nearest' });
}
