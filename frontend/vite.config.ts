import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

const docSlugs = [
  'getting-started', 'autonomy-spectrum', 'exploring-ideas', 'designing-specs',
  'board-and-tasks', 'automation', 'oversight-and-analytics', 'workspaces',
  'refinement-and-ideation', 'configuration', 'circuit-breakers',
  'usage', 'agents-and-flows',
];

export default defineConfig({
  plugins: [vue()],
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
      '/api':      { target: 'http://localhost:8080', changeOrigin: true },
      '/login':    'http://localhost:8080',
      '/callback': 'http://localhost:8080',
      '/logout':   'http://localhost:8080',
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
    include: ['src/**/*.test.ts'],
  },
});
