import type { RouteRecordRaw } from 'vue-router';

const cloudRoutes: RouteRecordRaw[] = [
  { path: '/', component: () => import('./views/ProductPage.vue') },
  { path: '/install', component: () => import('./views/InstallPage.vue') },
  { path: '/dashboard', component: () => import('./views/BoardPage.vue') },
  { path: '/:pathMatch(.*)*', component: () => import('./views/NotFoundPage.vue') },
];

// NOT_FOUND_ROUTE names the local catch-all, so a caller can tell whether a
// path resolves to a real console page (see the last-route restore in main.ts).
export const NOT_FOUND_ROUTE = 'not-found';

// needsWorkspace marks routes that render workspace-scoped data; App.vue
// shows the WorkspaceRequired prompt for these when no workspace is visible,
// so the board, plan/chat, routines, etc. stay consistent with /api/config's
// "no workspace" state. Settings, docs and the not-found page are
// workspace-independent.
export const localRoutes: RouteRecordRaw[] = [
  { path: '/', component: () => import('./views/BoardPage.vue'), meta: { needsWorkspace: true } },
  { path: '/routines', component: () => import('./views/RoutinesPage.vue'), meta: { needsWorkspace: true } },
  { path: '/analytics', component: () => import('./views/AnalyticsPage.vue'), meta: { needsWorkspace: true } },
  // /chat is the dedicated chat surface; /plan is the spec-mode page (kept as
  // "Plan" in the UI for non-technical friendliness — the route is unchanged).
  { path: '/chat', component: () => import('./views/ChatPage.vue'), meta: { needsWorkspace: true } },
  { path: '/plan', component: () => import('./views/PlanPage.vue'), meta: { needsWorkspace: true } },
  { path: '/whiteboard', component: () => import('./views/WhiteboardPage.vue'), meta: { needsWorkspace: true } },
  // Static artifacts gallery: lists and previews self-contained HTML under
  // <workspace>/artifacts/, served by the /artifact/<path> route.
  { path: '/artifacts', component: () => import('./views/ArtifactsView.vue'), meta: { needsWorkspace: true } },
  // The pipeline graph surface, renamed "Mission Control" in the UI; the
  // MapPage.vue component keeps its filename to avoid churn. /map redirects.
  { path: '/mission', component: () => import('./views/MapPage.vue'), meta: { needsWorkspace: true } },
  { path: '/map', redirect: '/mission', meta: { needsWorkspace: true } },
  { path: '/settings', component: () => import('./views/SettingsPage.vue') },
  { path: '/docs', component: () => import('./views/LocalDocsPage.vue') },
  { path: '/docs/:slug(.*)', component: () => import('./views/LocalDocsPage.vue') },
  // Any other path, including addresses of pages the console no longer has,
  // renders the not-found page inside the console shell.
  { path: '/:pathMatch(.*)*', name: NOT_FOUND_ROUTE, component: () => import('./views/LocalNotFoundPage.vue') },
];

function readMode(): 'local' | 'cloud' {
  if (typeof window !== 'undefined' && window.__WALLFACER__) {
    return window.__WALLFACER__.mode || 'cloud';
  }
  return 'cloud';
}

export const routes: RouteRecordRaw[] = readMode() === 'local' ? localRoutes : cloudRoutes;
