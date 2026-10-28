import { test, expect, type APIRequestContext } from "@playwright/test";
import { ensureNoPasswordViaAPI } from "./helpers";

// JSON-RPC over HTTP: the same handlers the data channel uses, reachable by
// scripts without a WebRTC session. The device runs in noPassword mode here,
// so no credentials are sent; api_test.go covers the auth paths.

async function rpc(request: APIRequestContext, method: string, params: Record<string, unknown> = {}) {
  const response = await request.post("/api/rpc", {
    data: { jsonrpc: "2.0", id: method, method, params },
  });
  expect(response.status(), `${method} status`).toBe(200);
  const body = await response.json();
  expect(body.error, `${method} error`).toBeUndefined();
  expect(body.id).toBe(method);
  return body.result;
}

test("JSON-RPC methods answer over HTTP", async ({ request }) => {
  await ensureNoPasswordViaAPI();

  const deviceId = await rpc(request, "getDeviceID");
  expect(typeof deviceId).toBe("string");
  expect(deviceId.length).toBeGreaterThan(0);

  const before = await rpc(request, "getJigglerState");
  try {
    await rpc(request, "setJigglerState", { enabled: !before });
    expect(await rpc(request, "getJigglerState")).toBe(!before);
  } finally {
    await rpc(request, "setJigglerState", { enabled: before });
  }

  const unknown = await request.post("/api/rpc", {
    data: { jsonrpc: "2.0", id: 1, method: "noSuchMethod" },
  });
  expect(unknown.status()).toBe(404);
  expect((await unknown.json()).error.code).toBe(-32601);
});
