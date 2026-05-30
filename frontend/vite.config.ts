import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

const wfProxy = process.env.WF_PROXY || 'http://localhost:8080';

const docSlugs = [
  'getting-started', 'autonomy-spectrum', 'exploring-ideas', 'designing-specs',
  'board-and-tasks', 'automation', 'oversight-and-analytics', 'workspaces',
  'refinement-and-ideation', 'configuration', 'circuit-breakers',
  'usage', 'agents-and-flows',
];

export default defineConfig({
  plugins: [vue()],
  // latere-ui ships source SFCs; compile them for the SSG build and keep one Vue copy.
  resolve: { dedupe: ['vue'] },
  // Pre-bundle the Vue ecosystem together so esbuild's lazy __esm init keeps
  // @vue/shared's helpers (isFunction, etc.) initialised before vue-router's
  // top-level defineComponent() calls run. Without this the optimized dev
  // chunks crash with "isFunction is not a function" and the app never mounts.
  optimizeDeps: { include: ['vue', 'pinia'], exclude: ['vue-router'] },
  ssr: { noExternal: ['latere-ui'] },
  ssgOptions: {
    includedRoutes(paths: string[]) {
      return [
        ...paths.filter(p => !p.includes(':') && !p.includes('*')),
        ...docSlugs.map(s => `/docs/${s}`),
      ];
    },
  },
  server: {
    port: 5173,
    fs: { allow: ['..'] },
    proxy: {
      // WF_PROXY overrides the backend target for dev (default: local wallfacer run on :8080).
      '/api':      { target: wfProxy, changeOrigin: true },
      '/login':    wfProxy,
      '/callback': wfProxy,
      '/logout':   wfProxy,
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
    target: 'es2022',
  },
  test: {
    environment: 'happy-dom',
    globals: false,
    include: ['src/**/*.test.ts', 'tests/**/*.test.ts'],
  },
});
