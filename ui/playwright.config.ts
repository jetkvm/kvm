import { defineConfig } from "@playwright/test";

if (!process.env.JETKVM_URL) {
  throw new Error("JETKVM_URL environment variable is required");
}

export default defineConfig({
  testDir: "./e2e",
  globalSetup: "./e2e/global-setup.ts",
  globalTeardown: "./e2e/global-teardown.ts",
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
      name: "ui",
      testIgnore: [/ota-.*/, /remote-agent\/.*/, /video-codec.*/],
    },
    {
      name: "video-codec",
      testMatch: /video-codec\.spec\.ts/,
    },
    {
      name: "remote-agent",
      testDir: "./e2e/remote-agent",
      testMatch: "ra-all.spec.ts",
    },
    { name: "ota-signed", testMatch: /ota-signature\.spec\.ts/, dependencies: ["remote-agent"] },
    {
      name: "ota-prerelease-unsigned",
      testMatch: /ota-prerelease-unsigned\.spec\.ts/,
      dependencies: ["remote-agent"],
    },
    {
      name: "ota-prerelease-rejected",
      testMatch: /ota-prerelease-rejected\.spec\.ts/,
      dependencies: ["remote-agent"],
    },
    {
      name: "ota-specific-version",
      testMatch: /ota-specific-version-unsigned\.spec\.ts/,
      dependencies: ["remote-agent"],
    },
    {
      name: "ota-upgrade-from-stable",
      testMatch: /ota-upgrade-from-stable\.spec\.ts/,
      dependencies: ["remote-agent"],
    },
    {
      name: "ota-upgrade-to-signed",
      testMatch: /ota-upgrade-to-signed\.spec\.ts/,
      dependencies: ["remote-agent"],
    },
  ],
});
