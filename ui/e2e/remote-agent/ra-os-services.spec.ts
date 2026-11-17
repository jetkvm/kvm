import { test, expect, type Page } from "@playwright/test";

import {
  callJsonRpc,
  ensureRpcReady,
  restartAppViaSSH,
  skipWithoutCapability,
  skipWithoutDeviceShell,
} from "../helpers";
import { agent, registerSharedSession, remoteHostExec } from "./shared";
import { waitForKeyboardReady } from "./remote-agent";

interface OsServicesSettings {
  enabled: boolean;
}

interface HostGadget {
  path: string;
  devnum: number;
  interfaces: string[];
  drives: string[];
}

const OS_SERVICES_INTERFACE = "ff/4a/01";
const INSTALL_LINK = "jetkvm.com/os-services/install";

let page: Page;
let original: OsServicesSettings | undefined;
test.afterAll(async () => {
  if (!page || !original) return;
  await callJsonRpc(page, "unmountImage");
  await callJsonRpc(
    page,
    "setOsServicesSettings",
    { settings: { enabled: original.enabled } },
    30_000,
  );
});

registerSharedSession(p => {
  page = p;
});
test.describe.configure({ mode: "serial" });

function hostGadgets(): HostGadget[] {
  return JSON.parse(
    remoteHostExec(`python3 - <<'PY'
import json
from pathlib import Path
gadgets = []
for d in Path('/sys/bus/usb/devices').glob('*-*'):
    if ':' in d.name:
        continue
    try:
        if 'jetkvm' not in (d / 'manufacturer').read_text().lower():
            continue
        devnum = int((d / 'devnum').read_text())
    except (OSError, ValueError):
        continue
    interfaces = []
    for i in sorted(d.glob(d.name + ':*')):
        try:
            interfaces.append('/'.join((i / n).read_text().strip() for n in ('bInterfaceClass', 'bInterfaceSubClass', 'bInterfaceProtocol')))
        except OSError:
            pass
    drives = sorted(b.name for b in d.glob(d.name + ':*/host*/target*/*/block/sr*'))
    gadgets.append({'path': str(d), 'devnum': devnum, 'interfaces': interfaces, 'drives': drives})
print(json.dumps(gadgets))
PY`),
  ) as HostGadget[];
}

function hostGadget(): HostGadget | null {
  const gadgets = hostGadgets();
  expect(gadgets.length, "the host must have at most one JetKVM").toBeLessThanOrEqual(1);
  return gadgets[0] ?? null;
}

async function setEnabled(enabled: boolean) {
  await callJsonRpc(page, "setOsServicesSettings", { settings: { enabled } }, 30_000);
}

async function waitForInterface(present: boolean): Promise<HostGadget> {
  await expect
    .poll(() => hostGadget()?.interfaces.includes(OS_SERVICES_INTERFACE) ?? null, {
      timeout: 30_000,
      intervals: [1000],
    })
    .toBe(present);
  return hostGadget()!;
}

test.beforeAll(async () => {
  await skipWithoutCapability(page, "os_services");
  original = (await callJsonRpc(page, "getOsServicesSettings")) as OsServicesSettings;
});

test("the host sees the OS Services interface only while the switch is on", async () => {
  test.setTimeout(120_000);

  await setEnabled(false);
  const off = await waitForInterface(false);

  await setEnabled(true);
  const on = await waitForInterface(true);
  expect(on.interfaces.length).toBe(off.interfaces.length + 1);
  expect((await waitForKeyboardReady(agent!, page, 30_000)).length).toBeGreaterThan(0);

  await setEnabled(false);
  const offAgain = await waitForInterface(false);
  expect(offAgain.interfaces).toEqual(off.interfaces);
  expect((await waitForKeyboardReady(agent!, page, 30_000)).length).toBeGreaterThan(0);
});

test("with OS Services on, an app restart enumerates the KVM again once and the keyboard works @ssh", async () => {
  test.setTimeout(150_000);
  await skipWithoutDeviceShell();

  await setEnabled(true);
  const before = (await waitForInterface(true)).devnum;

  await restartAppViaSSH();

  await expect
    .poll(() => hostGadget()?.devnum ?? null, { timeout: 30_000, intervals: [1000] })
    .not.toBe(before);
  const after = (await waitForInterface(true)).devnum;

  await page.goto("/", { waitUntil: "networkidle" });
  await ensureRpcReady(page);
  expect((await waitForKeyboardReady(agent!, page, 30_000)).length).toBeGreaterThan(0);
  expect(hostGadget()?.devnum, "the KVM must enumerate one time only").toBe(after);
});

test("the host reads the OS Services installer disc", async () => {
  test.setTimeout(120_000);

  await callJsonRpc(page, "unmountImage");
  await callJsonRpc(page, "mountBuiltinInstaller", {}, 30_000);

  await expect
    .poll(
      () => {
        const drive = hostGadget()?.drives[0];
        if (!drive) return 0;
        return Number(
          remoteHostExec(
            `sudo -n dd if=/dev/${drive} bs=2048 count=300 2>/dev/null | grep -a -c '${INSTALL_LINK}' || true`,
          ).trim(),
        );
      },
      { timeout: 30_000, intervals: [2000] },
    )
    .toBeGreaterThan(0);

  await callJsonRpc(page, "unmountImage");
});
