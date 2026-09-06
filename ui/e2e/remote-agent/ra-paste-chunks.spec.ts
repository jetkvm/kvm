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
  await expect(page.locator("textarea")).toBeVisible();
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
  await agent!.waitForInputDevices(["keyboard"], 30_000);
});

test.afterAll(async () => {
  if (page) await page.close();
});

test("a paste longer than one chunk lands on the host in full and in order", async () => {
  test.setTimeout(90_000);
  await agent!.clearKeyboardEvents();

  await openPasteModal();
  await page.locator("textarea").fill(TEXT);
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

  await page.keyboard.press("Escape");
});

test("cancelling a paste stops within one chunk and releases the keyboard", async () => {
  test.setTimeout(60_000);
  await agent!.clearKeyboardEvents();

  await openPasteModal();
  await page.locator("textarea").fill(TEXT);
  await page.getByRole("button", { name: "Confirm Paste" }).click();

  await expect
    .poll(async () => (await typedText()).length, { timeout: 15_000, intervals: [200] })
    .toBeGreaterThanOrEqual(20);

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
});
