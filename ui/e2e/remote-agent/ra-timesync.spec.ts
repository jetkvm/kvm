import { lookup } from "node:dns/promises";
import { spawn } from "node:child_process";
import { readFileSync } from "node:fs";
import { test, expect, type Page } from "@playwright/test";
import { callJsonRpc, ensureRpcReady, reconnectAfterReboot } from "../helpers";
import { registerSharedSession } from "./shared";

let page: Page;
let original: Record<string, unknown> | undefined;
let stopResponder: (() => Promise<void>) | undefined;

test.afterEach(async ({ browser }) => {
  test.setTimeout(90_000);
  try {
    if (original) {
      if (!page.isClosed()) await page.goto("about:blank");
      const recovery = await browser.newPage();
      try {
        await ensureRpcReady(recovery, { navigateFirst: true });
        await callJsonRpc(recovery, "setNetworkSettings", { settings: original });
        expect(await callJsonRpc(recovery, "getNetworkSettings")).toEqual(original);
      } finally {
        await recovery.close();
      }
    }
  } finally {
    await stopResponder?.();
  }
});
registerSharedSession(p => {
  page = p;
});
const quote = (s: string) => "'" + s.replace(/'/g, "'\\''") + "'";

test("custom NTP queries the configured host and persists across reboot @network", async () => {
  test.setTimeout(240_000);
  page.setDefaultTimeout(10_000);
  const target = process.env.JETKVM_REMOTE_HOST!;
  const host = target.split("@").at(-1)!;
  const { address: source } = await lookup(new URL(process.env.JETKVM_URL!).hostname, {
    family: 4,
  });
  const script = readFileSync(
    new URL("../../../e2e/remote-agent/ntp_server.py", import.meta.url),
    "utf8",
  );
  original = (await callJsonRpc(page, "getNetworkSettings")) as Record<string, unknown>;
  const child = spawn("ssh", [
    "-o",
    "BatchMode=yes",
    "-o",
    "ConnectTimeout=10",
    target,
    `sudo -n python3 -u -c ${quote(script)} ${quote(source)}`,
  ]);
  let output = "",
    errors = "";
  let closed = false;
  const exited = new Promise<void>(resolve =>
    child.once("close", () => {
      closed = true;
      resolve();
    }),
  );
  child.stdout.on("data", chunk => {
    output += chunk.toString();
  });
  child.stderr.on("data", chunk => {
    errors += chunk.toString();
  });
  child.on("error", error => {
    errors += String(error);
  });
  const count = () => (output.match(/"request": true/g) || []).length;
  stopResponder = async () => {
    child.stdin.end();
    await Promise.race([exited, new Promise(resolve => setTimeout(resolve, 5000))]);
    if (!closed) child.kill("SIGTERM");
    expect(closed, "NTP responder must stop after its owner closes").toBe(true);
  };
  await expect
    .poll(() => {
      if (closed) throw new Error(`NTP responder failed: ${errors}`);
      return output.includes('"ready": true');
    })
    .toBe(true);
  // Start from a different configuration so saving must trigger a fresh query,
  // even when this host was already the user's selected server.
  await callJsonRpc(page, "setNetworkSettings", {
    settings: {
      ...original,
      time_sync_mode: "ntp_only",
      time_sync_ntp_servers: [],
      time_sync_http_urls: [],
      // Keep this test on its responder; public fallback has separate coverage.
      time_sync_disable_fallback: true,
    },
  });
  await page.waitForTimeout(2000); // allow deferred settings application to finish
  // Exercise the shared settings UI, not only its RPC.
  await page.goto("/settings/network");
  await ensureRpcReady(page);
  await page.locator('[name="time_sync_mode"]').selectOption("custom");
  const servers = page.locator('input[name^="time_sync_ntp_servers."]');
  await expect(servers.first()).toBeVisible();
  const entries = await servers.all();
  for (const entry of entries) await entry.fill(host);
  const configuredServers = entries.map(() => host);
  await page.getByRole("button", { name: "Save Settings", exact: true }).first().click();
  // NTP changes save directly; only connectivity changes prompt for confirmation.
  await expect
    .poll(() => callJsonRpc(page, "getNetworkSettings"))
    .toMatchObject({
      time_sync_mode: "custom",
      time_sync_ntp_servers: configuredServers,
      time_sync_disable_fallback: true,
    });
  await expect.poll(count, { timeout: 90_000 }).toBeGreaterThan(0);
  const before = count();
  await callJsonRpc(page, "reboot", { force: true });
  await reconnectAfterReboot(page);
  expect(await callJsonRpc(page, "getNetworkSettings")).toMatchObject({
    time_sync_mode: "custom",
    time_sync_ntp_servers: configuredServers,
    time_sync_disable_fallback: true,
  });
  await expect.poll(count, { timeout: 90_000 }).toBeGreaterThan(before);
});
