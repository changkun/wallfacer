import { ViteSSG } from 'vite-ssg';
import { createPinia } from 'pinia';
import App from './App.vue';
import { routes } from './router';
import './styles/tokens.css';
import './styles/board-tokens.css';
import './styles/base.css';
import './styles/buttons.css';
import './styles/forms.css';
import './styles/badges.css';
import './styles/header.css';
import './styles/board.css';
import './styles/status-bar.css';
import './styles/task-detail.css';
import './styles/app.css';

export const createApp = ViteSSG(App, { routes }, ({ app }) => {
  app.use(createPinia());
});
