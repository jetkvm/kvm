import { test, expect, type Page } from "@playwright/test";
import {
  callJsonRpc,
  ensureNoPasswordViaAPI,
  ensureRpcReady,
  waitForWebRTCReady,
} from "../helpers";
import { KEY, createRemoteAgent } from "./remote-agent";

// A paste is sent to the device as keyboard macro reports of at most 128 wire
// steps each, and the next report only goes out once the device reports the
// previous one finished. These tests pin what that buys: every character lands
// on the host in order across chunk boundaries, no single data channel message
// grows with the text, and a cancel stops within one chunk.

const agent = createRemoteAgent();

test.describe.configure({ mode: "serial" });

let page: Page;

// 128 wire steps at 9 bytes each plus the 6 byte header.
const MAX_MACRO_MESSAGE_BYTES = 128 * 9 + 6;

const LETTERS = "abcdefghij";
const LETTER_CODES: Record<string, number> = {
  a: KEY.A,
  b: KEY.B,
  c: KEY.C,
  d: KEY.D,
  e: KEY.E,
  f: KEY.F,
  g: KEY.G,
  h: KEY.H,
  i: KEY.I,
  j: KEY.J,
};
const CODE_LETTERS = Object.fromEntries(
  Object.entries(LETTER_CODES).map(([letter, code]) => [code, letter]),
);

// 200 characters is three full chunks and a partial one.
const TEXT = LETTERS.repeat(20);

async function typedText(): Promise<string> {
  const events = await agent!.getKeyboardEvents();
  return events
    .filter(e => e.type === "key_press")
    .map(e => CODE_LETTERS[e.code] ?? "?")
    .join("");
}

async function openPasteModal(): Promise<void> {
  await page.getByRole("button", { name: "Paste text" }).click();
  await expect(page.locator('textarea[rows="4"]')).toBeVisible();
}

test.beforeAll(async ({ browser }) => {
  test.skip(!agent, "JETKVM_REMOTE_HOST not set");
  await Promise.all([agent!.ensureDeployed(), ensureNoPasswordViaAPI()]);

  page = await browser.newPage();
  // Record the largest hidrpc message the page sends.
  await page.addInitScript(() => {
    const send = RTCDataChannel.prototype.send;
    (window as unknown as { __maxHidRpcMessage: number }).__maxHidRpcMessage = 0;
    RTCDataChannel.prototype.send = function (this: RTCDataChannel, data: never) {
      if (this.label === "hidrpc") {
        const size = (data as ArrayBuffer).byteLength ?? 0;
        const w = window as unknown as { __maxHidRpcMessage: number };
        w.__maxHidRpcMessage = Math.max(w.__maxHidRpcMessage, size);
      }
      return send.call(this, data);
    };
  });
  await page.goto("/", { waitUntil: "networkidle" });
  await waitForWebRTCReady(page);
  await ensureRpcReady(page);
  // Terminal channels settle focus shortly after the initial connection.
  await page.waitForTimeout(1_000);
  await expect
    .poll(async () => (await agent!.getJetKVMInputDevices()).some(d => d.type === "keyboard"), {
      timeout: 30_000,
    })
    .toBe(true);
});

test.afterAll(async () => {
  if (page) await page.close();
});

test("a paste longer than one chunk lands on the host in full and in order", async () => {
  test.setTimeout(90_000);
  await agent!.clearKeyboardEvents();

  await openPasteModal();
  await page.locator('textarea[rows="4"]').fill(TEXT, { timeout: 5_000 });
  const confirm = page.getByRole("button", { name: "Confirm Paste" });
  await confirm.click();

  await expect
    .poll(async () => (await typedText()).length, {
      message: "host should receive every character",
      timeout: 60_000,
      intervals: [500],
    })
    .toBeGreaterThanOrEqual(TEXT.length);

  expect(await typedText()).toBe(TEXT);

  // The paste is reported finished once the last chunk is done.
  await expect(confirm).toBeEnabled({ timeout: 5_000 });

  const maxMessage = await page.evaluate(
    () => (window as unknown as { __maxHidRpcMessage: number }).__maxHidRpcMessage,
  );
  expect(maxMessage, "no hidrpc message should carry more than one chunk").toBeLessThanOrEqual(
    MAX_MACRO_MESSAGE_BYTES,
  );

  await page.getByRole("button", { name: "Paste text" }).click();
  await expect(page.locator('textarea[rows="4"]')).toHaveCount(0);
});

test("cancelling after reopening the paste popover stops and releases the keyboard", async () => {
  test.setTimeout(60_000);
  await agent!.clearKeyboardEvents();

  await openPasteModal();
  await page.locator('textarea[rows="4"]').fill(TEXT, { timeout: 5_000 });
  await page.getByRole("button", { name: "Confirm Paste" }).click();

  await expect
    .poll(async () => (await typedText()).length, { timeout: 15_000, intervals: [200] })
    .toBeGreaterThanOrEqual(20);

  await page.getByRole("button", { name: "Paste text" }).click();
  await expect(page.locator('textarea[rows="4"]')).toHaveCount(0);
  await openPasteModal();

  await page
    .getByRole("button", { name: /^cancel$/i })
    .last()
    .click();

  // Whatever the device had queued in the current chunk may still land, but
  // nothing beyond it, and nothing keeps arriving afterwards.
  await page.waitForTimeout(1_000);
  const afterCancel = await typedText();
  await page.waitForTimeout(3_000);
  expect(await typedText()).toBe(afterCancel);
  expect(afterCancel.length).toBeLessThan(TEXT.length);
  expect(TEXT.startsWith(afterCancel)).toBe(true);

  const keysDown = (await callJsonRpc(page, "getKeyDownState")) as { keys: number[] };
  expect(keysDown.keys.filter(k => k !== 0)).toEqual([]);
  await expect(page.locator('textarea[rows="4"]')).toHaveCount(0);
});

test("a toolbar macro cannot be interrupted by chunks from a previous paste", async () => {
  test.setTimeout(60_000);
  const saved = (await callJsonRpc(page, "getKeyboardMacros")) as object[];
  const replacement = {
    id: "e2e_test_replacement",
    name: "E2E Replacement B",
    sortOrder: 0,
    steps: Array.from({ length: 10 }, () => ({ keys: ["KeyB"], modifiers: [], delay: 50 })),
  };
  try {
    await callJsonRpc(page, "setKeyboardMacros", { params: { macros: [...saved, replacement] } });
    await page.reload({ waitUntil: "networkidle" });
    await waitForWebRTCReady(page);
    await ensureRpcReady(page);
    await page.waitForTimeout(1_000);
    await agent!.clearKeyboardEvents();
    await openPasteModal();
    await page.locator('textarea[rows="4"]').fill("a".repeat(200));
    await page.getByRole("button", { name: "Confirm Paste" }).click();
    await expect.poll(typedText, { timeout: 10_000 }).toMatch(/^a{10,199}$/);

    // Closing the popover leaves the paste running. A different hook instance
    // in the toolbar must cancel it before starting the replacement macro.
    await page.getByRole("button", { name: "Paste text" }).click();
    await expect(page.locator('textarea[rows="4"]')).toHaveCount(0);
    await page.getByRole("button", { name: replacement.name, exact: true }).click();
    await expect.poll(typedText, { timeout: 15_000 }).toMatch(/^a{10,199}b{10}$/);
    const completed = await typedText();
    await page.waitForTimeout(5_000);
    expect(await typedText()).toBe(completed);
    const keysDown = (await callJsonRpc(page, "getKeyDownState")) as { keys: number[] };
    expect(keysDown.keys.filter(k => k !== 0)).toEqual([]);
  } finally {
    await callJsonRpc(page, "setKeyboardMacros", { params: { macros: saved } });
  }
});
