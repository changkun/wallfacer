import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    environment: "node",
    include: ["js/tests/**/*.test.{js,ts}"],
    coverage: {
      include: ["js/**/*.{js,ts}"],
      exclude: ["js/vendor/**", "js/tests/**", "js/**/*.d.ts"],
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
