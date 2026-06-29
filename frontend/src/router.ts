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
  // The agent-graph surface is the single place to define agents AND compose
  // them into graphs (it embeds the agent editor). It supersedes both the old
  // Flows composer and the Agents page, so /agents, /workflows and /flows all
  // redirect here (e2e design: teardown of the legacy flow + agents surfaces).
  { path: '/agent-graph', component: () => import('./views/AgentGraphPage.vue'), meta: { needsWorkspace: true } },
  { path: '/agents', redirect: '/agent-graph', meta: { needsWorkspace: true } },
  { path: '/workflows', redirect: '/agent-graph', meta: { needsWorkspace: true } },
  { path: '/flows', redirect: '/agent-graph', meta: { needsWorkspace: true } },
  { path: '/routines', component: () => import('./views/RoutinesPage.vue'), meta: { needsWorkspace: true } },
  { path: '/github', component: () => import('./views/GithubPage.vue'), meta: { needsWorkspace: true } },
  { path: '/analytics', component: () => import('./views/AnalyticsPage.vue'), meta: { needsWorkspace: true } },
  // /chat is the dedicated chat surface; /plan is the spec-mode page (kept as
  // "Plan" in the UI for non-technical friendliness — the route is unchanged).
  { path: '/chat', component: () => import('./views/ChatPage.vue'), meta: { needsWorkspace: true } },
  { path: '/plan', component: () => import('./views/PlanPage.vue'), meta: { needsWorkspace: true } },
  { path: '/whiteboard', component: () => import('./views/WhiteboardPage.vue'), meta: { needsWorkspace: true } },
  // The pipeline graph surface, renamed "Mission Control" in the UI; the
  // MapPage.vue component keeps its filename to avoid churn. /map redirects.
  { path: '/mission', component: () => import('./views/MapPage.vue'), meta: { needsWorkspace: true } },
  { path: '/map', redirect: '/mission', meta: { needsWorkspace: true } },
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
