import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "node",
    include: ["js/tests/**/*.test.js"],
    coverage: {
      include: ["js/**/*.js"],
      exclude: ["js/vendor/**", "js/tests/**"],
      reporter: ["text"],
      thresholds: {
        statements: 0.5,
        branches: 0.5,
        functions: 0.5,
        lines: 0.5,
      },
    },
  },
});
