// The v-scrollfade directive adds `is-scrolling` while a container is scrolled
// and clears it after an idle timeout, so the scrollbar CSS can fade the thumb
// in and out. It must also add the base `scrollfade` class on mount and detach
// its listener on unmount.
import { it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, h, withDirectives, type App } from 'vue';
import { vScrollFade } from './scrollFade';

let app: App | null = null;
let host: HTMLElement;

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  if (app) app.unmount();
  app = null;
  host?.remove();
  vi.useRealTimers();
});

it('adds the base class on mount and toggles is-scrolling on scroll', () => {
  host = document.createElement('div');
  document.body.appendChild(host);
  app = createApp({
    render: () => withDirectives(h('div', { class: 'box' }), [[vScrollFade]]),
  });
  const vm = app.mount(host);
  const el = vm.$el as HTMLElement;

  // Base class applied immediately.
  expect(el.classList.contains('scrollfade')).toBe(true);
  expect(el.classList.contains('is-scrolling')).toBe(false);

  // A scroll event reveals the bar.
  el.dispatchEvent(new Event('scroll'));
  expect(el.classList.contains('is-scrolling')).toBe(true);

  // It stays visible while scrolling continues (timer keeps resetting)...
  vi.advanceTimersByTime(700);
  el.dispatchEvent(new Event('scroll'));
  vi.advanceTimersByTime(700);
  expect(el.classList.contains('is-scrolling')).toBe(true);

  // ...and fades out after the idle window elapses.
  vi.advanceTimersByTime(800);
  expect(el.classList.contains('is-scrolling')).toBe(false);
});

it('detaches its scroll listener on unmount', () => {
  host = document.createElement('div');
  document.body.appendChild(host);
  app = createApp({
    render: () => withDirectives(h('div', { class: 'box' }), [[vScrollFade]]),
  });
  const vm = app.mount(host);
  const el = vm.$el as HTMLElement;
  app.unmount();
  app = null;

  // After unmount a scroll must not re-add the class (listener gone).
  el.dispatchEvent(new Event('scroll'));
  expect(el.classList.contains('is-scrolling')).toBe(false);
});
