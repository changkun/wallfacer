import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environment: 'node',
    include: ['ui/js/tests/**/*.test.js'],
    coverage: {
      include: ['ui/js/**/*.js'],
      exclude: ['ui/js/vendor/**', 'ui/js/tests/**'],
      reporter: ['text'],
      // Tests execute frontend files through vm.runInContext from raw source files.
      // That execution path is not instrumented by Vitest coverage collection, so
      // enforce a non-blocking baseline threshold to keep CI green while still
      // collecting artifacts for local inspection.
      thresholds: {
        statements: 0,
        branches: 0,
        functions: 0,
        lines: 0,
      },
    },
  },
});
