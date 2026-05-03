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
