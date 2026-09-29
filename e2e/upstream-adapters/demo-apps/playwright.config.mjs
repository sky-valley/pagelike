// Configuration for running demo-apps' own tests/demo-pages.spec.js (unchanged)
// against pagelike (docs/spec/apps.md §8.1). Same settings as the upstream
// playwright.config.mjs, plus the system Chrome channel (no browser download)
// and one worker.
import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  timeout: 30_000,
  retries: 0,
  workers: 1,
  reporter: [["line"], ["json", { outputFile: "results.json" }]],
  use: {
    reducedMotion: "reduce",
    trace: "off",
    channel: process.env.PW_CHANNEL || "chrome",
  },
});
