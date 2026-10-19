import type { RouteRecordRaw } from 'vue-router';

// NOT_FOUND_ROUTE names the catch-all, so a caller can tell whether a
// path resolves to a real console page (see the last-route restore in main.ts).
export const NOT_FOUND_ROUTE = 'not-found';

// needsWorkspace marks routes that render workspace-scoped data; App.vue
// shows the WorkspaceRequired prompt for these when no workspace is visible,
// so the board and plan stay consistent with /api/config's
// "no workspace" state. Settings and the not-found page are
// workspace-independent.
export const routes: RouteRecordRaw[] = [
  { path: '/', component: () => import('./views/BoardPage.vue'), meta: { needsWorkspace: true } },
  // /plan is the spec-mode page, named "Plan" in the UI.
  { path: '/plan', component: () => import('./views/PlanPage.vue'), meta: { needsWorkspace: true } },
  { path: '/settings', component: () => import('./views/SettingsPage.vue') },
  // Any other path, including addresses of pages the console no longer has,
  // renders the not-found page inside the console shell.
  { path: '/:pathMatch(.*)*', name: NOT_FOUND_ROUTE, component: () => import('./views/LocalNotFoundPage.vue') },
];
