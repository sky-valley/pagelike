import { defineConfig } from "@playwright/test";

// The global setup builds pagelike, starts it on a free port with a fresh
// data directory, and exports PAGELIKE_* variables to the tests.
export default defineConfig({
  testDir: "./tests",
  timeout: 60_000,
  retries: 0,
  workers: 1,
  reporter: [["line"]],
  globalSetup: "./global-setup.mjs",
  globalTeardown: "./global-teardown.mjs",
  use: {
    trace: "retain-on-failure",
    channel: process.env.PW_CHANNEL || "chrome",
    // *.localhost must resolve to loopback inside Chromium (it does by default).
  },
});
