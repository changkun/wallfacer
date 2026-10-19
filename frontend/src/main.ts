import { createApp } from 'vue';
import { createRouter, createWebHistory } from 'vue-router';
import { createHead } from '@unhead/vue/client';
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
import './styles/primitives.css';
import './styles/board.css';
import './styles/modal.css';
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
import './styles/syntax.css';
import './styles/utilities.css';
import './styles/scroll-fade.css';
import './styles/globals.css';
import './styles/brand-override.css';
import { vScrollFade } from './directives/scrollFade';

const router = createRouter({ history: createWebHistory(import.meta.env.BASE_URL), routes });
const app = createApp(App);
app.use(createPinia());
app.use(router);
app.use(createHead());
app.directive('scrollfade', vScrollFade);

// Remember where the user left off. Persist each navigation, then on a cold
// launch that landed on the default board restore the last route. A stale path
// degrades gracefully; see lib/lastRoute.ts. The stored route is captured
// before afterEach overwrites it with the initial landing (which fires for the
// first navigation too).
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

// Mount once the initial navigation has resolved, so components that read the
// route during setup (the workspace URL sync in AppLayout) see the requested
// page rather than the start location.
void router.isReady().then(() => app.mount('#app'));
