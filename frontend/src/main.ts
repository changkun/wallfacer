import { ViteSSG } from 'vite-ssg';
import { createPinia } from 'pinia';
import App from './App.vue';
import { routes } from './router';
import { rememberRoute, routeToRestore, storedRoute } from './lib/lastRoute';
import './styles/tokens.css';
import './styles/board-tokens.css';
import './styles/base.css';
import './styles/animations.css';
import './styles/header.css';
import './styles/status-bar.css';
import './styles/dock.css';
import './styles/badges.css';
import './styles/forms.css';
import './styles/buttons.css';
import './styles/board.css';
import './styles/modal.css';
import './styles/settings-modal.css';
import './styles/settings-page.css';
import './styles/task-detail.css';
import './styles/mermaid.css';
import './styles/diffs.css';
import './styles/multi-turn.css';
import './styles/oversight.css';
import './styles/mentions.css';
import './styles/search.css';
import './styles/command-palette.css';
import './styles/workspace-picker.css';
import './styles/explorer.css';
import './styles/spec-mode.css';
import './styles/docs.css';
import './styles/agents.css';
import './styles/flows.css';
import './styles/syntax.css';
import './styles/utilities.css';
import './styles/app.css';

export const createApp = ViteSSG(App, { routes }, ({ app, router, isClient }) => {
  app.use(createPinia());

  // Remember where the user left off (local mode only — cloud `/` is the
  // marketing page). Persist each navigation, then on a cold launch that
  // landed on the default board restore the last route. A stale path degrades
  // gracefully; see lib/lastRoute.ts.
  if (isClient && typeof window !== 'undefined' && window.__WALLFACER__?.mode === 'local') {
    // Capture the stored route before afterEach overwrites it with the initial
    // landing (which fires for the first navigation too).
    const previous = storedRoute();
    router.afterEach((to) => rememberRoute(to.fullPath));
    void router.isReady().then(() => {
      const target = routeToRestore(router.currentRoute.value.fullPath, previous);
      if (target) void router.replace(target).catch(() => {});
    });
  }
  // Browser RUM: only enable in cloud mode — that's the only deployment
  // where the backend exposes /v1/telemetry/* (TelemetryProxy). Local-mode
  // binaries (and dev) have no proxy, so initialising here would spam the
  // console with 405s from every OTLP batch.
  if (
    isClient &&
    import.meta.env.PROD &&
    typeof window !== 'undefined' &&
    window.__WALLFACER__?.mode === 'cloud'
  ) {
    // Lazy: otel + zone.js (~130 kB) would otherwise bloat the eager entry
    // chunk for every user, including local mode where telemetry never runs.
    void import('./telemetry').then(({ initTelemetry }) => initTelemetry('wallfacer-spa'));
  }
});
