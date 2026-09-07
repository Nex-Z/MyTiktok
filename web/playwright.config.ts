import { defineConfig, devices } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  timeout: 45000,
  workers: 1,
  fullyParallel: false,
  use: { baseURL: "http://127.0.0.1:18080", trace: "retain-on-failure" },
  projects: [
    {
      name: "desktop",
      use: {
        ...devices["Desktop Chrome"],
        viewport: { width: 1440, height: 960 },
      },
    },
    { name: "mobile", use: { ...devices["Pixel 7"] } },
  ],
  webServer: {
    command: "node tests/server.mjs",
    url: "http://127.0.0.1:18080/api/health",
    timeout: 120000,
    reuseExistingServer: false,
  },
});
