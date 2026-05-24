import { ViteSSG } from 'vite-ssg';
import { createPinia } from 'pinia';
import App from './App.vue';
import { routes } from './router';
import { initTelemetry } from './telemetry';
import './styles/tokens.css';
import './styles/board-tokens.css';
import './styles/base.css';
import './styles/header.css';
import './styles/status-bar.css';
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

export const createApp = ViteSSG(App, { routes }, ({ app, isClient }) => {
  app.use(createPinia());
  // Browser RUM: client-side only (this setup also runs in Node during SSG)
  // and production-only (no telemetry proxy in dev).
  if (isClient && import.meta.env.PROD) {
    initTelemetry('wallfacer-spa');
  }
});
