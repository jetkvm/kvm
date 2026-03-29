import { defineConfig } from "@playwright/test";

if (!process.env.JETKVM_URL) {
  throw new Error("JETKVM_URL environment variable is required");
}

export default defineConfig({
  testDir: ".",
  globalSetup: "../e2e/global-setup.ts",
  globalTeardown: "../e2e/global-teardown.ts",
  timeout: 600_000,
  workers: 1,
  reporter: [["list", { printSteps: true }]],
  use: {
    baseURL: process.env.JETKVM_URL,
    trace: "retain-on-failure",
    video: "retain-on-failure",
    screenshot: "only-on-failure",
  },
});
