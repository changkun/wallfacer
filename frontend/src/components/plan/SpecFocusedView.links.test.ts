// Regression test: relative .md spec links inside the focused body must be
// intercepted and turned into a focus-sibling navigation. The click handler used
// to be attached in onMounted against bodyRef, but the body is gated behind a
// v-if and is absent at mount whenever its content resolves AFTER mount (the
// async spec fetch, or a prompt that arrives post-mount). So the listener never
// attached and clicking a spec link did nothing. The handler is now bound
// declaratively via @click on the body div, so Vue attaches it when the body
// renders. This test populates the body AFTER mount to reproduce that timing.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createRouter, createMemoryHistory, type Router } from 'vue-router';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import SpecFocusedView from './SpecFocusedView.vue';
import { usePlanningStore } from '../../stores/planning';

vi.mock('../../lib/mermaidRender', () => ({
  enhanceMermaid: vi.fn(),
  watchThemeReinit: vi.fn(),
}));

async function flushUntil(pred: () => boolean, tries = 60) {
  for (let i = 0; i < tries && !pred(); i++) {
    await nextTick();
    await new Promise((r) => setTimeout(r, 0));
  }
}

describe('SpecFocusedView spec-link interception', () => {
  let app: App | null = null;
  let el: HTMLElement;
  let router: Router;
  let pinia: Pinia;

  beforeEach(() => {
    pinia = createPinia();
    setActivePinia(pinia);
    router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div/>' } }],
    });
    el = document.createElement('div');
    document.body.appendChild(el);
  });

  afterEach(() => {
    app?.unmount();
    app = null;
    el.remove();
  });

  it('intercepts a relative .md link rendered into the body after mount', async () => {
    const planning = usePlanningStore();
    planning.focusedTaskId = 't1';
    planning.focusedTaskTitle = 'Task';
    // Empty at mount: the body (with ref="bodyRef") is absent, exactly when the
    // old onMounted listener would have had a null bodyRef and never attached.
    planning.focusedTaskPrompt = '';

    router.push('/');
    await router.isReady();

    const onFocusSibling = vi.fn();
    app = createApp(SpecFocusedView, { chatVisible: true, onFocusSibling });
    app.use(router);
    app.use(pinia);
    app.mount(el);
    await nextTick();

    // Now the content arrives, after mount — the body element appears here.
    planning.focusedTaskPrompt = 'See [other spec](other.md) for details.';

    await flushUntil(() => !!el.querySelector('a[href="other.md"]'));
    const link = el.querySelector('a[href="other.md"]') as HTMLAnchorElement;
    expect(link).toBeTruthy();

    link.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }));
    await nextTick();

    expect(onFocusSibling).toHaveBeenCalledWith('other.md');
  });
});
