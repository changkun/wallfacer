// v-scrollfade — an opt-in overlay-scrollbar behaviour for a single scroll
// container: the bar stays hidden and only fades in while the element is being
// scrolled (or hovered so the thumb is grabbable), then fades back out after a
// short idle.
//
// Why a directive rather than the native default: the app leaves scrollbars to
// the OS on purpose (see base.css) because styling them opts out of macOS
// overlay auto-hide. That default only auto-hides for users on "Show scroll
// bars: When scrolling"; anyone on "Always" gets a permanent classic bar. This
// directive re-creates the auto-hide look for the few surfaces that want it
// (the chat stream and session list) without changing the global default.
//
// The visible/hidden toggle is a class the CSS animates (see scroll-fade.css);
// background-color on the thumb transitions in Chromium where opacity would not.
import type { Directive } from 'vue';

const IDLE_MS = 800;

interface ScrollFadeState {
  onScroll: () => void;
  timer: ReturnType<typeof setTimeout> | null;
}

const registry = new WeakMap<HTMLElement, ScrollFadeState>();

export const vScrollFade: Directive<HTMLElement> = {
  mounted(el) {
    el.classList.add('scrollfade');
    const state: ScrollFadeState = { onScroll: () => {}, timer: null };
    state.onScroll = () => {
      el.classList.add('is-scrolling');
      if (state.timer !== null) clearTimeout(state.timer);
      state.timer = setTimeout(() => el.classList.remove('is-scrolling'), IDLE_MS);
    };
    el.addEventListener('scroll', state.onScroll, { passive: true });
    registry.set(el, state);
  },
  beforeUnmount(el) {
    const state = registry.get(el);
    if (!state) return;
    el.removeEventListener('scroll', state.onScroll);
    if (state.timer !== null) clearTimeout(state.timer);
    registry.delete(el);
  },
};
