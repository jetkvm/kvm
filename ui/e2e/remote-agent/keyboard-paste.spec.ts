/**
 * Paste round-trip tests.
 *
 * Drives the same paste pipeline the PasteModal uses: fetches the active
 * keyboard layout via JSON-RPC, builds a scancode-based KeyboardMacroStep[]
 * from the charMap (mirroring PasteModal.onConfirmPaste), then executes it
 * through the executeHidMacro test hook. The remote agent verifies that the
 * host received the expected key-press sequence.
 *
 * Catches regressions in: layout charMap correctness (Go), JSON-RPC layout
 * payload, paste macro construction (TS), HID-RPC transport, USB delivery.
 *
 * Run with:
 *   JETKVM_URL=http://<kvm-ip> JETKVM_REMOTE_HOST=<host-ip> \
 *     npx playwright test keyboard-paste --project=remote-agent
 */
import { test, expect, type Page } from "@playwright/test";
import { callJsonRpc, getDeviceHost, goToSession, restartAppViaSSH, sshExec } from "../helpers";
import {
  createRemoteAgent,
  KEY,
  HID_TO_LINUX,
  type KeyboardEvent as RAKeyboardEvent,
} from "./remote-agent";

const HID_KEY_BUFFER_SIZE = 6;

interface HIDCombo {
  s: number; // scancode
  m: number; // modifier byte
  p?: HIDCombo; // dead-key prefix
}

interface KeyboardLayout {
  id: string;
  name: string;
  charMap: Record<string, HIDCombo>;
}

interface MacroStep {
  keys: number[];
  modifier: number;
  delay: number;
}

const agent = createRemoteAgent();
const layoutCache = new Map<string, KeyboardLayout>();

async function getLayout(page: Page, id: string): Promise<KeyboardLayout> {
  const cached = layoutCache.get(id);
  if (cached) return cached;
  const layout = (await callJsonRpc(page, "getKeyboardLayoutData", { id })) as KeyboardLayout;
  layoutCache.set(id, layout);
  return layout;
}

/**
 * Build a paste macro from text — mirrors PasteModal.onConfirmPaste verbatim
 * so a passing test here implies the production paste path works the same way.
 */
function buildPasteMacro(layout: KeyboardLayout, text: string, delay = 20): MacroStep[] {
  const reset: MacroStep = {
    keys: new Array(HID_KEY_BUFFER_SIZE).fill(0),
    modifier: 0,
    delay,
  };
  const macro: MacroStep[] = [];
  for (const { segment } of new Intl.Segmenter().segment(text)) {
    const normalized = segment.normalize("NFC");
    const combo = layout.charMap[normalized];
    if (!combo || combo.s === 0) continue;
    if (combo.p) {
      macro.push({ keys: [combo.p.s], modifier: combo.p.m, delay: 20 });
      macro.push({ ...reset });
    }
    macro.push({ keys: [combo.s], modifier: combo.m, delay: 20 });
    macro.push({ ...reset });
  }
  return macro;
}

async function pasteText(page: Page, layoutId: string, text: string): Promise<MacroStep[]> {
  const layout = await getLayout(page, layoutId);
  const macro = buildPasteMacro(layout, text);
  await page.evaluate(steps => window.__kvmTestHooks!.executeHidMacro(steps), macro);
  return macro;
}

/**
 * Wait until the remote agent reports key_press events covering all expected
 * key codes in order (other key_press events between them are ignored — paste
 * with shift modifier produces interleaved shift/key press events).
 */
async function waitForKeyPresses(expected: number[], timeoutMs = 5000): Promise<RAKeyboardEvent[]> {
  const deadline = Date.now() + timeoutMs;
  let lastPresses: number[] = [];
  while (Date.now() < deadline) {
    const events = await agent!.getKeyboardEvents();
    const presses = events.filter(ev => ev.type === "key_press");
    lastPresses = presses.map(ev => ev.code);
    let idx = 0;
    for (const code of lastPresses) {
      if (code === expected[idx]) {
        idx++;
        if (idx === expected.length) return presses;
      }
    }
    await new Promise(r => setTimeout(r, 100));
  }
  throw new Error(
    `Timed out (${timeoutMs}ms) waiting for key sequence [${expected.join(", ")}]; got [${lastPresses.join(", ")}]`,
  );
}

/** Convert a charMap-built MacroStep[] to expected Linux key codes. */
function macroToLinuxCodes(macro: MacroStep[]): number[] {
  const codes: number[] = [];
  for (const step of macro) {
    for (const sc of step.keys) {
      if (sc === 0) continue;
      const linux = HID_TO_LINUX[sc];
      if (linux !== undefined) codes.push(linux);
    }
  }
  return codes;
}

/**
 * Ensure the device has its config wiped to noPassword mode so the WebRTC
 * session can be opened without going through the welcome flow. Mirrors the
 * helper in ra-all.spec.ts.
 */
async function ensureNoPasswordViaAPI() {
  const host = getDeviceHost();
  const status = await fetch(`http://${host}/device/status`).then(
    r => r.json() as Promise<{ isSetup: boolean }>,
  );
  if (!status.isSetup) {
    const res = await fetch(`http://${host}/device/setup`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ localAuthMode: "noPassword" }),
    });
    if (!res.ok) throw new Error(`Setup POST failed: ${res.status}`);
    return;
  }
  const probe = await fetch(`http://${host}/device`);
  if (probe.status === 401) {
    await sshExec("rm -f /userdata/kvm_config.json && sync");
    await restartAppViaSSH();
    const res = await fetch(`http://${host}/device/setup`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ localAuthMode: "noPassword" }),
    });
    if (!res.ok) throw new Error(`Setup POST after reset failed: ${res.status}`);
  }
}

test.describe.configure({ mode: "serial" });

let sharedPage: Page;

test.beforeAll(async ({ browser }) => {
  test.skip(!agent, "JETKVM_REMOTE_HOST not set");
  test.setTimeout(60_000);

  await Promise.all([agent!.ensureDeployed(), ensureNoPasswordViaAPI()]);

  sharedPage = await browser.newPage();
  await goToSession(sharedPage);
  await agent!.waitForInputDevices(["keyboard", "absolute_mouse", "relative_mouse"], 30000);
});

test.afterAll(async () => {
  await sharedPage?.close();
});

test.beforeEach(async () => {
  await agent!.clearKeyboardEvents();
});

test.describe("paste", () => {
  test("regression: literal space character is delivered (not silently dropped)", async () => {
    // Was a bug: layouts had Space's legend normalized to "Space" (5 chars),
    // so addChar rejected it on the rune-count==1 check and " " never made it
    // into charMap. PasteModal would then skip every space silently.
    const macro = await pasteText(sharedPage, "en-US", "a b");
    expect(macro.length).toBeGreaterThan(0);
    await waitForKeyPresses([KEY.A, KEY.SPACE, KEY.B]);
  });

  test("plain ASCII with multiple words and spaces", async () => {
    await pasteText(sharedPage, "en-US", "hi there");
    await waitForKeyPresses([KEY.H, KEY.I, KEY.SPACE, KEY.T, KEY.H, KEY.E, KEY.R, KEY.E]);
  });

  test("uppercase letters trigger the shift modifier", async () => {
    const macro = await pasteText(sharedPage, "en-US", "Hi");
    // Macro must include a step with the shift modifier byte (USB HID modifier
    // bit 0x02 = LeftShift) for the H key — verifies charMap.m is non-zero.
    const hStep = macro.find(s => s.keys.includes(0x0b)); // 0x0B = HID H
    expect(hStep, "macro should contain a step that presses H").toBeDefined();
    expect(hStep!.modifier & 0x02).toBe(0x02);
    // And the host receives both the shift and the H key.
    const presses = await waitForKeyPresses([KEY.H, KEY.I]);
    const codes = presses.map(ev => ev.code);
    expect(codes).toContain(KEY.LEFT_SHIFT);
  });

  test("multi-line: Enter is delivered between lines", async () => {
    await pasteText(sharedPage, "en-US", "a\nb");
    await waitForKeyPresses([KEY.A, KEY.ENTER, KEY.B]);
  });

  test("digits and basic punctuation", async () => {
    await pasteText(sharedPage, "en-US", "a1.,;");
    await waitForKeyPresses([KEY.A, KEY.KEY_1, KEY.DOT, KEY.COMMA, KEY.SEMICOLON]);
  });

  test("invalid characters are silently skipped (valid chars still delivered)", async () => {
    // ☃ snowman is not in any built-in layout's charMap.
    await pasteText(sharedPage, "en-US", "a☃b");
    await waitForKeyPresses([KEY.A, KEY.B]);
    // And no extra noise — only A, then B (modulo the modifier-clearing reset).
    const events = await agent!.getKeyboardEvents();
    const printable = events
      .filter(ev => ev.type === "key_press")
      .filter(ev => ev.code === KEY.A || ev.code === KEY.B || ev.code === KEY.SPACE);
    expect(printable.length).toBe(2);
  });
});

test.describe("paste: layout-specific charMap", () => {
  test("de-DE layout: ÄÖÜ are delivered (not silently dropped like en-US would)", async () => {
    // German umlauts are in de-DE charMap but absent from en-US. The macro
    // construction succeeds with de-DE; pasting through en-US would yield 0 steps.
    const enMacro = await pasteText(sharedPage, "en-US", "äöü");
    expect(enMacro.length, "en-US has no umlauts in charMap, macro should be empty").toBe(0);

    await agent!.clearKeyboardEvents();
    const deMacro = await pasteText(sharedPage, "de-DE", "äöü");
    expect(deMacro.length, "de-DE charMap should have entries for äöü").toBeGreaterThan(0);

    // Each umlaut must produce at least one HID key event reaching the host.
    // Don't assert specific Linux key codes — the host's input layer maps the
    // scancodes to whatever its own keyboard layout dictates, and our test
    // host is en-US. We just verify the keystrokes arrive.
    const linuxCodes = macroToLinuxCodes(deMacro).filter(c => c !== KEY.LEFT_SHIFT);
    await waitForKeyPresses(linuxCodes);
  });

  test("de-DE QWERTZ vs en-US QWERTY: 'y' goes to different scancodes", async () => {
    // Verifies layout switching actually changes the macro. The same letter
    // 'y' is on different physical keys in the two layouts.
    const enLayout = await getLayout(sharedPage, "en-US");
    const deLayout = await getLayout(sharedPage, "de-DE");
    const enY = enLayout.charMap["y"];
    const deY = deLayout.charMap["y"];
    expect(enY, "en-US charMap['y']").toBeDefined();
    expect(deY, "de-DE charMap['y']").toBeDefined();
    expect(enY!.s, "en-US 'y' and de-DE 'y' use different HID scancodes").not.toBe(deY!.s);
  });

  test("dead-key composition: fr-FR composes â as ^ then a", async () => {
    const layout = await getLayout(sharedPage, "fr-FR");
    const combo = layout.charMap["â"];
    expect(combo, "fr-FR charMap should contain â").toBeDefined();
    expect(combo!.p, "â must be a dead-key composition (^ then a)").toBeDefined();

    const macro = await pasteText(sharedPage, "fr-FR", "â");
    // Macro: prefix-step (^), reset, base-step (a), reset.
    // Both prefix and base produce key_press events on the host.
    const linuxCodes = macroToLinuxCodes(macro).filter(c => c !== KEY.LEFT_SHIFT);
    expect(linuxCodes.length, "â paste should produce two distinct key events").toBe(2);
    await waitForKeyPresses(linuxCodes);
  });
});
