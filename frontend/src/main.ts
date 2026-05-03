import { ViteSSG } from 'vite-ssg';
import { createPinia } from 'pinia';
import App from './App.vue';
import { routes } from './router';
import './styles/tokens.css';
import './styles/app.css';

export const createApp = ViteSSG(App, { routes }, ({ app }) => {
  app.use(createPinia());
});
