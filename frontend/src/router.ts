import type { RouteRecordRaw } from 'vue-router';

const cloudRoutes: RouteRecordRaw[] = [
  { path: '/', component: () => import('./views/ProductPage.vue') },
  { path: '/pricing', component: () => import('./views/PricingPage.vue') },
  { path: '/docs', component: () => import('./views/DocsIndex.vue') },
  { path: '/docs/:slug', component: () => import('./views/DocPage.vue'), props: true },
  { path: '/install', component: () => import('./views/InstallPage.vue') },
  { path: '/dashboard', component: () => import('./views/BoardPage.vue') },
  { path: '/:pathMatch(.*)*', component: () => import('./views/NotFoundPage.vue') },
];

const localRoutes: RouteRecordRaw[] = [
  { path: '/', component: () => import('./views/BoardPage.vue') },
  { path: '/agents', component: () => import('./views/AgentsPage.vue') },
  { path: '/flows', component: () => import('./views/FlowsPage.vue') },
  { path: '/analytics', component: () => import('./views/AnalyticsPage.vue') },
  { path: '/plan', component: () => import('./views/PlanPage.vue') },
  { path: '/explorer', component: () => import('./views/ExplorerPage.vue') },
  { path: '/settings', component: () => import('./views/SettingsPage.vue') },
];

function readMode(): 'local' | 'cloud' {
  if (typeof window !== 'undefined' && window.__WALLFACER__) {
    return window.__WALLFACER__.mode || 'cloud';
  }
  return 'cloud';
}

export const routes: RouteRecordRaw[] = readMode() === 'local' ? localRoutes : cloudRoutes;
