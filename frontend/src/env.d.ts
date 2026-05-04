/// <reference types="vite/client" />

declare module '*.vue' {
  import type { DefineComponent } from 'vue';
  const component: DefineComponent<object, object, unknown>;
  export default component;
}

declare const __WALLFACER_VERSION__: string;

interface WallfacerBootConfig {
  mode: 'local' | 'cloud';
  serverApiKey: string;
  version: string;
}

interface Window {
  __WALLFACER__?: WallfacerBootConfig;
}

// Legacy IIFE modules vendored from ui/js/ (depgraph, unified-graph) have
// no exports; Vite imports them for side effects so they attach renderer
// functions to `window`. The shape of those window functions is declared
// at the call site (frontend/src/views/MapPage.vue).
declare module '*/ui/js/depgraph.js';
declare module '*/ui/js/unified-graph.js';
