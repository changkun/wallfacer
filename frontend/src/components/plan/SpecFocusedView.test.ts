// Regression test for mermaid diagrams rendering as raw code in the body.
//
// The body-change watch had no `immediate: true`, so when the rendered body is
// already non-empty at mount (the focused-task prompt is populated before the
// keyed component mounts) the watch never fired and the
// `.mermaid-block[data-mermaid]` placeholders stayed as raw <pre>. Adding
// `{ immediate: true }` runs enhanceMermaid once on the initial mount.
//
// We drive the watch via the focused-task prompt branch rather than the spec
// body: the prompt is a store ref settable synchronously before mount, giving
// non-empty content at watcher-registration time and no post-mount change. The
// spec body loads async (fetch), so its '' -> content transition fires the
// watch even without `immediate`, which cannot distinguish the fix. This branch
// is the real "already computed at mount" case the bug describes.
// (enhanceMermaid itself is stubbed, it needs a real browser to lay out SVG;
// the bug under test is that it was never called.)
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createRouter, createMemoryHistory, type Router } from 'vue-router';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import SpecFocusedView from './SpecFocusedView.vue';
import { useAgentStore } from '../../stores/agentSession';

// vi.hoisted so the spy exists when the hoisted vi.mock factory runs.
const { enhanceMermaid } = vi.hoisted(() => ({ enhanceMermaid: vi.fn() }));
vi.mock('../../lib/mermaidRender', () => ({
  enhanceMermaid,
  watchThemeReinit: vi.fn(),
}));

const PROMPT = 'Intro paragraph.\n\n```mermaid\ngraph TD; A-->B;\n```\n';

async function flushUntil(pred: () => boolean, tries = 40) {
  for (let i = 0; i < tries && !pred(); i++) {
    await nextTick();
    await new Promise((r) => setTimeout(r, 0));
  }
}

describe('SpecFocusedView mermaid rendering', () => {
  let app: App | null = null;
  let el: HTMLElement;
  let router: Router;
  let pinia: Pinia;

  beforeEach(() => {
    enhanceMermaid.mockClear();
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

  it('enhances mermaid placeholders already rendered at mount', async () => {
    // Focused-task prompt populated before mount: renderedTaskPrompt is
    // non-empty at watcher registration and never changes afterward.
    const planning = useAgentStore();
    planning.focusedTaskId = 't1';
    planning.focusedTaskTitle = 'Task';
    planning.focusedTaskPrompt = PROMPT;

    router.push('/');
    await router.isReady();
    app = createApp(SpecFocusedView, { chatVisible: true });
    app.use(router);
    app.use(pinia);
    app.mount(el);

    await flushUntil(() => enhanceMermaid.mock.calls.length > 0);

    expect(enhanceMermaid).toHaveBeenCalled();
    const container = enhanceMermaid.mock.calls[0][0] as HTMLElement;
    expect(container).toBeTruthy();
    expect(container.querySelector('.mermaid-block')).toBeTruthy();
  });
});
