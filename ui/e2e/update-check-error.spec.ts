import * as http from "http";
import type { AddressInfo } from "net";
import { test, expect } from "@playwright/test";

import {
  configureDeviceUpdateUrl,
  getLocalNetworkIP,
  restartAppViaSSH,
  restoreDeviceUpdateUrl,
  skipWithoutDeviceShell,
} from "./helpers";

/**
 * The update check runs on the device. Point it at a local server that answers
 * every request with 500: the update dialog must show the device's reason for
 * the failure, not only that the check failed.
 */
test.describe("Update check failure", () => {
  let server: http.Server | undefined;

  test.beforeAll(async () => {
    await skipWithoutDeviceShell();
    server = http.createServer((_request, response) => {
      response.writeHead(500);
      response.end();
    });
    await new Promise<void>(resolve => server!.listen(0, "0.0.0.0", resolve));
    const { port } = server.address() as AddressInfo;
    await configureDeviceUpdateUrl(`http://${getLocalNetworkIP()}:${port}`);
    await restartAppViaSSH();
  });

  test.afterAll(async () => {
    if (server) {
      await restoreDeviceUpdateUrl();
      await restartAppViaSSH();
      server.close();
    }
  });

  test("the update dialog shows why the check failed", async ({ page }) => {
    await page.goto("/settings/general/update");
    await expect(page.getByText(/unexpected status code: 500/)).toBeVisible({ timeout: 60000 });
  });
});
