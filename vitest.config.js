import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environment: 'node',
    include: ['ui/js/tests/**/*.test.js'],
  },
});
