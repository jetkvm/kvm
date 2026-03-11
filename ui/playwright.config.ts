import { defineConfig } from "@playwright/test";

if (!process.env.JETKVM_URL) {
  throw new Error("JETKVM_URL environment variable is required");
}

export default defineConfig({
  testDir: "./e2e",
  timeout: 60000,
  workers: 1,
  reporter: [["list", { printSteps: true }]],
  use: {
    baseURL: process.env.JETKVM_URL,
    trace: "retain-on-failure",
    video: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "core",
      testIgnore: /ota-.*/,
    },
    {
      name: "ota-signed",
      testMatch: /ota-signature\.spec\.ts/,
    },
    {
      name: "ota-prerelease-unsigned",
      testMatch: /ota-prerelease-unsigned\.spec\.ts/,
    },
    {
      name: "ota-prerelease-rejected",
      testMatch: /ota-prerelease-rejected\.spec\.ts/,
    },
    {
      name: "ota-specific-version",
      testMatch: /ota-specific-version-unsigned\.spec\.ts/,
    },
  ],
});
