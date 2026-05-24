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

  // Shims the legacy depgraph / unified-graph renderers (vendored from ui/js/)
  // expect, installed by MapPage.vue. Declared here (ambient) rather than via a
  // `declare global` inside MapPage.vue so test files that touch these globals
  // typecheck under vue-tsc without importing the SFC.
  specModeState?: {
    tree: Array<{
      path: string;
      spec: { status: string; dispatched_task_id: string | null };
      children: string[];
      is_leaf: boolean;
      depth: number;
    }>;
    index: unknown;
  };
  depGraphEnabled?: boolean;
  openTaskModal?: (id: string) => void;
  focusSpec?: (path: string) => void;
  switchMode?: (mode: string, opts?: { persist?: boolean }) => void;
  scheduleRender?: () => void;
  renderDependencyGraph?: (tasks: import('./api/types').Task[]) => void;
  hideDependencyGraph?: () => void;
  setMapShowArchived?: (v: boolean) => void;
  setMapSearch?: (q: string) => void;
  resetMapLayout?: () => void;
  _resetMapCentering?: () => void;
}

// Legacy IIFE modules vendored from ui/js/ (depgraph, unified-graph) have
// no exports; Vite imports them for side effects so they attach renderer
// functions to `window` (typed above).
declare module '*/ui/js/depgraph.js';
declare module '*/ui/js/unified-graph.js';
