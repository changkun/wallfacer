// Regression test for mermaid diagrams not rendering in the in-app docs viewer.
//
// The markdown renderer emits `.mermaid-block[data-mermaid]` placeholders, but
// LocalDocsPage never ran the mermaid post-processor on the rendered body, so
// diagrams stayed as raw placeholders. This proves the viewer enhances them
// after a doc loads. (enhanceMermaid itself is stubbed — it needs a real browser
// to lay out SVG; the bug under test is that it was never called.)
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createRouter, createMemoryHistory, type Router } from 'vue-router';
import LocalDocsPage from './LocalDocsPage.vue';

// vi.hoisted so these spies exist when the hoisted vi.mock factory runs.
const { enhanceMermaid, watchThemeReinit } = vi.hoisted(() => ({
  enhanceMermaid: vi.fn(),
  watchThemeReinit: vi.fn(),
}));
vi.mock('../lib/mermaidRender', () => ({ enhanceMermaid, watchThemeReinit }));

// Docs list comes from api(); doc content from global fetch().
vi.mock('../api/client', () => ({
  api: vi.fn(async () => [{ slug: 'guide/demo', title: 'Demo', category: 'guide', order: 1 }]),
}));

const MERMAID_DOC = '# Demo\n\nIntro paragraph.\n\n```mermaid\ngraph TD; A-->B;\n```\n';

async function flushUntil(pred: () => boolean, tries = 40) {
  for (let i = 0; i < tries && !pred(); i++) {
    await nextTick();
    await new Promise((r) => setTimeout(r, 0));
  }
}

describe('LocalDocsPage mermaid rendering', () => {
  let app: App | null = null;
  let el: HTMLElement;
  let router: Router;

  beforeEach(() => {
    enhanceMermaid.mockClear();
    watchThemeReinit.mockClear();
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => new Response(MERMAID_DOC, { status: 200, headers: { 'Content-Type': 'text/markdown' } })),
    );
    router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/docs/:slug(.*)?', component: { template: '<div/>' } }],
    });
    el = document.createElement('div');
    document.body.appendChild(el);
  });

  afterEach(() => {
    app?.unmount();
    app = null;
    el.remove();
    vi.unstubAllGlobals();
  });

  it('enhances mermaid placeholders after a doc loads', async () => {
    router.push('/docs/guide/demo');
    await router.isReady();
    app = createApp(LocalDocsPage);
    app.use(router);
    app.mount(el);

    await flushUntil(() => enhanceMermaid.mock.calls.length > 0);

    expect(enhanceMermaid).toHaveBeenCalled();
    const container = enhanceMermaid.mock.calls[0][0] as HTMLElement;
    expect(container).toBeTruthy();
    expect(container.querySelector('.mermaid-block')).toBeTruthy();
  });
});
