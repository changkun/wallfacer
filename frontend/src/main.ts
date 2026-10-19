import { ViteSSG } from 'vite-ssg';
import { createPinia } from 'pinia';
import App from './App.vue';
import { NOT_FOUND_ROUTE, routes } from './router';
import { rememberRoute, routeToRestore, storedRoute } from './lib/lastRoute';
import './styles/tokens.css';
import './styles/palettes.css';
import './styles/base.css';
import './styles/animations.css';
import './styles/rail.css';
import './styles/topbar.css';
import './styles/dock.css';
import './styles/primitives.css';
import './styles/board.css';
import './styles/modal.css';
import './styles/analytics.css';
import './styles/settings-page.css';
import './styles/task-detail.css';
import './styles/mermaid.css';
import './styles/diffs.css';
import './styles/multi-turn.css';
import './styles/search.css';
import './styles/command-palette.css';
import './styles/workspace-picker.css';
import './styles/explorer.css';
import './styles/spec-mode.css';
import './styles/docs.css';
import './styles/mission.css';
import './styles/syntax.css';
import './styles/utilities.css';
import './styles/scroll-fade.css';
import './styles/globals.css';
import './styles/brand-override.css';
import { vScrollFade } from './directives/scrollFade';

export const createApp = ViteSSG(App, { routes }, ({ app, router, isClient }) => {
  app.use(createPinia());
  app.directive('scrollfade', vScrollFade);

  // Remember where the user left off. Persist each navigation, then on a cold
  // launch that landed on the default board restore the last route. A stale
  // path degrades gracefully; see lib/lastRoute.ts.
  if (isClient) {
    // Capture the stored route before afterEach overwrites it with the initial
    // landing (which fires for the first navigation too).
    const previous = storedRoute();
    router.afterEach((to) => rememberRoute(to.fullPath));
    void router.isReady().then(() => {
      const target = routeToRestore(
        router.currentRoute.value.fullPath,
        previous,
        (path) => router.resolve(path).name !== NOT_FOUND_ROUTE,
      );
      if (target) void router.replace(target).catch(() => {});
    });
  }
});
