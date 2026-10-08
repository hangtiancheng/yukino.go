import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  fullyParallel: false,
  workers: 1,
  use: {
    baseURL: process.env.TASKFLOW_TEST_URL || "http://127.0.0.1:15173",
    viewport: { width: 1440, height: 960 },
    screenshot: "only-on-failure",
  },
  webServer: process.env.TASKFLOW_TEST_URL
    ? undefined
    : {
        command: "pnpm exec vite --host 127.0.0.1 --port 15173 --strictPort",
        url: "http://127.0.0.1:15173",
        reuseExistingServer: false,
      },
});
