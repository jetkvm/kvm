import { test, expect, type Page } from "@playwright/test";
import { callJsonRpc, ensureNoPasswordViaAPI, ensureRpcReady } from "./helpers";

// Network settings a device can only change when it reports the capability.
const SETTINGS = [
  { capability: "http_proxy", field: "http_proxy" },
  { capability: "domain", field: "domain" },
  { capability: "mdns", field: "mdns_mode" },
  { capability: "ipv6", field: "ipv6_mode" },
];

// What a device with fixed network settings reports, as the JetKVM Mini does.
const FIXED = {
  dhcp_client: "lwip",
  http_proxy: "",
  domain: "local",
  mdns_mode: "disabled",
  ipv6_mode: "disabled",
};

async function openNetworkSettings(page: Page) {
  await ensureRpcReady(page, { navigateFirst: true });
  await page.getByRole("button", { name: "Settings", exact: true }).click();
  await page.getByRole("link", { name: "Network", exact: true }).click();
}

test("the JetKVM reports the network settings it can change, and the page shows them", async ({
  page,
}) => {
  await ensureNoPasswordViaAPI();
  await openNetworkSettings(page);
  const capabilities = (await callJsonRpc(page, "getDeviceCapabilities")) as string[];
  for (const { capability, field } of SETTINGS) {
    expect(capabilities).toContain(capability);
    await expect(page.locator(`[name="${field}"]`)).toBeVisible();
  }
  await expect(page.locator('select[name="dhcp_client"]')).toBeVisible();
});

test("a device without them gets no controls for them, and a save keeps their values", async ({
  page,
}) => {
  await ensureNoPasswordViaAPI();
  // Make the JetKVM answer like a device with fixed network settings: drop the
  // capabilities from what it reports, and report the FIXED values. A save is
  // answered here and never reaches the device.
  await page.addInitScript(
    ({ removed, fixed }) => {
      const create = RTCPeerConnection.prototype.createDataChannel;
      RTCPeerConnection.prototype.createDataChannel = function (label, options) {
        const channel = create.call(this, label, options);
        if (label !== "rpc") return channel;
        const methods = new Map<unknown, string>();
        const rewrite = (data: string) => {
          const message = JSON.parse(data);
          const method = message.method ?? methods.get(message.id);
          const keep = (c: string) => !removed.includes(c);
          if (method === "deviceCapabilities") message.params = message.params.filter(keep);
          if (method === "getDeviceCapabilities") message.result = message.result.filter(keep);
          if (method === "getNetworkSettings") Object.assign(message.result, fixed);
          return JSON.stringify(message);
        };
        const wrappers = new Map<unknown, EventListener>();
        const add = channel.addEventListener.bind(channel);
        const remove = channel.removeEventListener.bind(channel);
        channel.addEventListener = ((type: string, listener: EventListener, options?: boolean) => {
          if (type !== "message") return add(type, listener, options);
          const wrapper = (event: Event) =>
            listener.call(
              channel,
              new MessageEvent("message", { data: rewrite((event as MessageEvent).data) }),
            );
          wrappers.set(listener, wrapper);
          return add(type, wrapper, options);
        }) as typeof channel.addEventListener;
        channel.removeEventListener = ((type: string, listener: EventListener, options?: boolean) =>
          remove(
            type,
            wrappers.get(listener) ?? listener,
            options,
          )) as typeof channel.removeEventListener;
        const send = channel.send.bind(channel);
        channel.send = ((data: string) => {
          const request = JSON.parse(data);
          methods.set(request.id, request.method);
          if (request.method !== "setNetworkSettings") return send(data);
          (window as unknown as { __saved: unknown }).__saved = request.params.settings;
          const reply = { jsonrpc: "2.0", id: request.id, result: request.params.settings };
          setTimeout(() =>
            channel.dispatchEvent(new MessageEvent("message", { data: JSON.stringify(reply) })),
          );
        }) as typeof channel.send;
        return channel;
      };
    },
    { removed: SETTINGS.map(s => s.capability), fixed: FIXED },
  );

  await openNetworkSettings(page);
  // The DHCP client shows as text once the settings have loaded.
  await expect(page.getByText(FIXED.dhcp_client, { exact: true })).toBeVisible({
    timeout: 20_000,
  });
  await expect(page.locator('[name="dhcp_client"]')).toHaveCount(0);
  for (const { field } of SETTINGS) await expect(page.locator(`[name="${field}"]`)).toHaveCount(0);

  // The hidden settings are still sent with the values the device reported,
  // so a device that refuses changes to them accepts the save.
  await page.locator('[name="hostname"]').fill("jetkvm-e2e-capabilities");
  await page.getByRole("button", { name: "Save Settings", exact: true }).first().click();
  await page.getByRole("button", { name: "Apply changes", exact: true }).click();
  const saved = () => page.evaluate(() => (window as unknown as { __saved?: unknown }).__saved);
  await expect.poll(saved).toBeTruthy();
  expect(await saved()).toMatchObject(FIXED);
});
