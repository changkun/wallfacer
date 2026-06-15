import type { RouteRecordRaw } from 'vue-router';

const cloudRoutes: RouteRecordRaw[] = [
  { path: '/', component: () => import('./views/ProductPage.vue') },
  { path: '/docs', component: () => import('./views/DocsIndex.vue') },
  { path: '/docs/:slug', component: () => import('./views/DocPage.vue'), props: true },
  { path: '/install', component: () => import('./views/InstallPage.vue') },
  { path: '/dashboard', component: () => import('./views/BoardPage.vue') },
  { path: '/:pathMatch(.*)*', component: () => import('./views/NotFoundPage.vue') },
];

// needsWorkspace marks routes that render workspace-scoped data; App.vue
// shows the WorkspaceRequired prompt for these when no workspace is visible,
// so the board, plan/chat, agents, etc. stay consistent with /api/config's
// "no workspace" state. Settings and docs are workspace-independent.
export const localRoutes: RouteRecordRaw[] = [
  { path: '/', component: () => import('./views/BoardPage.vue'), meta: { needsWorkspace: true } },
  { path: '/agents', component: () => import('./views/AgentsPage.vue'), meta: { needsWorkspace: true } },
  { path: '/workflows', component: () => import('./views/FlowsPage.vue'), meta: { needsWorkspace: true } },
  { path: '/flows', redirect: '/workflows' },
  { path: '/routines', component: () => import('./views/RoutinesPage.vue'), meta: { needsWorkspace: true } },
  { path: '/analytics', component: () => import('./views/AnalyticsPage.vue'), meta: { needsWorkspace: true } },
  // /chat is the dedicated chat surface; /plan is the spec-mode page (kept as
  // "Plan" in the UI for non-technical friendliness — the route is unchanged).
  { path: '/chat', component: () => import('./views/ChatPage.vue'), meta: { needsWorkspace: true } },
  { path: '/plan', component: () => import('./views/PlanPage.vue'), meta: { needsWorkspace: true } },
  { path: '/map', component: () => import('./views/MapPage.vue'), meta: { needsWorkspace: true } },
  { path: '/settings', component: () => import('./views/SettingsPage.vue') },
  { path: '/docs', component: () => import('./views/LocalDocsPage.vue') },
  { path: '/docs/:slug(.*)', component: () => import('./views/LocalDocsPage.vue') },
];

function readMode(): 'local' | 'cloud' {
  if (typeof window !== 'undefined' && window.__WALLFACER__) {
    return window.__WALLFACER__.mode || 'cloud';
  }
  return 'cloud';
}

export const routes: RouteRecordRaw[] = readMode() === 'local' ? localRoutes : cloudRoutes;
