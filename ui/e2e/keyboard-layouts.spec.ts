/**
 * Keyboard layout management e2e tests (no host required).
 *
 * Covers the layout administration surface:
 *  - getKeyboardLayouts returns all built-ins
 *  - setKeyboardLayout / getKeyboardLayout round-trip persists
 *  - POST /keyboard/upload installs a custom KLE; getKeyboardLayouts
 *    surfaces it; deleteKeyboardLayout removes it
 *  - deleteKeyboardLayout refuses built-ins
 *  - settings page renders the uploaded layout with delete/preview buttons
 *
 * Run with:
 *   JETKVM_URL=http://<kvm-ip> npx playwright test keyboard-layouts --project=ui
 */
import { test, expect, type Page } from "@playwright/test";
import { callJsonRpc, ensureLocalAuthMode, getDeviceHost, goToSession, sshExec } from "./helpers";

const TEST_LAYOUT_ID = "e2e-test-layout";

// Minimal KLE: a single A key. Enough to exercise parse + store.
// (The schema requires only an array of rows, each an array of mixed metadata
// objects and string legends.)
const MINIMAL_KLE_JSON = JSON.stringify([[{ name: "E2E Test Layout" }], ["a"]]);

interface LayoutMeta {
  id: string;
  name: string;
  builtin?: boolean;
}

interface UploadResponse {
  id: string;
  name: string;
  keyCount: number;
  warnings?: string[];
}

async function uploadTestLayout(name?: string, replaceId?: string): Promise<UploadResponse> {
  const host = getDeviceHost();
  const params = new URLSearchParams();
  if (name) params.set("name", name);
  if (replaceId) params.set("id", replaceId);
  const qs = params.toString();
  const url = `http://${host}/keyboard/upload${qs ? "?" + qs : ""}`;
  const res = await fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: MINIMAL_KLE_JSON,
  });
  if (!res.ok) {
    throw new Error(`upload failed: ${res.status} ${await res.text()}`);
  }
  return (await res.json()) as UploadResponse;
}

async function getLayouts(page: Page): Promise<LayoutMeta[]> {
  return (await callJsonRpc(page, "getKeyboardLayouts")) as LayoutMeta[];
}

/** Best-effort cleanup: try to delete via RPC, then by SSH if reachable. */
async function deleteTestLayoutQuietly(page: Page): Promise<void> {
  try {
    await callJsonRpc(page, "deleteKeyboardLayout", { id: TEST_LAYOUT_ID });
  } catch {
    /* not present */
  }
  // Belt-and-braces: clean the file directly so a partial test run can't
  // poison the next iteration.
  await sshExec(`rm -f /userdata/kvm_layouts/${TEST_LAYOUT_ID}.layout.json`, true).catch(() => {});
}

test.describe.configure({ mode: "serial" });

let sharedPage: Page;

test.beforeAll(async ({ browser }) => {
  test.setTimeout(60_000);
  sharedPage = await browser.newPage();
  await ensureLocalAuthMode(sharedPage, { mode: "noPassword" });
  await goToSession(sharedPage);
  await deleteTestLayoutQuietly(sharedPage);
});

test.afterAll(async () => {
  await deleteTestLayoutQuietly(sharedPage);
  await sharedPage?.close();
});

test.describe("layouts: built-ins", () => {
  test("getKeyboardLayouts surfaces a meaningful set of built-ins", async () => {
    const layouts = await getLayouts(sharedPage);
    expect(Array.isArray(layouts)).toBe(true);
    expect(layouts.length, "should ship multiple layouts").toBeGreaterThan(5);

    // Spot-check the layouts the paste tests rely on.
    const ids = layouts.map(l => l.id);
    for (const id of ["en-US", "de-DE", "fr-FR"]) {
      expect(ids, `built-in ${id}`).toContain(id);
    }
  });

  test("deleteKeyboardLayout refuses built-in layouts", async () => {
    await expect(callJsonRpc(sharedPage, "deleteKeyboardLayout", { id: "en-US" })).rejects.toThrow(
      /cannot delete built-in/i,
    );
  });
});

test.describe("layouts: setKeyboardLayout persistence", () => {
  test("setKeyboardLayout to a built-in is reflected by getKeyboardLayout", async () => {
    await callJsonRpc(sharedPage, "setKeyboardLayout", { layout: "de-DE" });
    const current = await callJsonRpc(sharedPage, "getKeyboardLayout");
    expect(current).toBe("de-DE");

    // Restore to en-US so other test suites aren't surprised.
    await callJsonRpc(sharedPage, "setKeyboardLayout", { layout: "en-US" });
  });
});

test.describe("layouts: custom KLE upload + delete", () => {
  test("upload installs the layout, getKeyboardLayouts lists it, delete removes it", async () => {
    // Upload — Go assigns an ID derived from the name when no id query is set.
    // To get a known ID we replace into TEST_LAYOUT_ID.
    const result = await uploadTestLayout("E2E Test Layout", TEST_LAYOUT_ID);
    expect(result.id).toBe(TEST_LAYOUT_ID);
    expect(result.keyCount).toBeGreaterThan(0);

    // The list should now include it.
    let layouts = await getLayouts(sharedPage);
    expect(
      layouts.find(l => l.id === TEST_LAYOUT_ID),
      "uploaded layout in list",
    ).toBeDefined();

    // Layout data is fetchable.
    const data = (await callJsonRpc(sharedPage, "getKeyboardLayoutData", {
      id: TEST_LAYOUT_ID,
    })) as { id: string; charMap: Record<string, unknown> };
    expect(data.id).toBe(TEST_LAYOUT_ID);
    expect(data.charMap["a"], "uploaded layout has 'a' in charMap").toBeDefined();

    // Delete and verify it's gone.
    await callJsonRpc(sharedPage, "deleteKeyboardLayout", { id: TEST_LAYOUT_ID });
    layouts = await getLayouts(sharedPage);
    expect(
      layouts.find(l => l.id === TEST_LAYOUT_ID),
      "layout removed",
    ).toBeUndefined();
  });
});

test.describe("layouts: settings UI", () => {
  test("uploaded layout renders with delete and preview buttons", async () => {
    await uploadTestLayout("E2E Test Layout", TEST_LAYOUT_ID);

    await sharedPage.goto("/settings/keyboard");
    await sharedPage.waitForLoadState("networkidle");

    const deleteBtn = sharedPage.getByTestId(`delete-layout-${TEST_LAYOUT_ID}`);
    const previewBtn = sharedPage.getByTestId(`preview-layout-${TEST_LAYOUT_ID}`);
    await expect(deleteBtn, "delete button visible for custom layout").toBeVisible({
      timeout: 10000,
    });
    await expect(previewBtn, "preview button visible for custom layout").toBeVisible({
      timeout: 5000,
    });

    // Cleanup via API rather than UI to keep the test focused.
    await callJsonRpc(sharedPage, "deleteKeyboardLayout", { id: TEST_LAYOUT_ID });
  });
});
