import type { RouteRecordRaw } from 'vue-router';

export const routes: RouteRecordRaw[] = [
  { path: '/', component: () => import('./views/HomePage.vue') },
  { path: '/pricing', component: () => import('./views/PricingPage.vue') },
  { path: '/docs', component: () => import('./views/DocsIndex.vue') },
  { path: '/docs/:slug', component: () => import('./views/DocPage.vue'), props: true },
  { path: '/install', component: () => import('./views/InstallPage.vue') },
  { path: '/dashboard', component: () => import('./views/BoardPage.vue') },
  { path: '/:pathMatch(.*)*', component: () => import('./views/NotFoundPage.vue') },
];
