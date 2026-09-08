import { expect, type Page } from "@playwright/test";
import { callJsonRpc, ensureRpcReady } from "./device";

export interface UsbConfig {
  vendor_id: string;
  product_id: string;
  serial_number: string;
  manufacturer: string;
  product: string;
}

export interface MediaState {
  source: string;
  mode: string;
  filename?: string;
  url?: string;
}

export interface HardwareState {
  config: UsbConfig;
  devices: Record<string, boolean>;
  enabled: boolean;
  audio: { enabled: boolean } | null;
  media: MediaState | null;
}

export async function captureHardwareState(page: Page): Promise<HardwareState> {
  // Sequential RPCs avoid competing with USB transitions on small devices.
  const config = (await callJsonRpc(page, "getUsbConfig")) as UsbConfig;
  const devices = (await callJsonRpc(page, "getUsbDevices")) as Record<string, boolean>;
  const enabled = (await callJsonRpc(page, "getUsbEmulationState")) as boolean;
  let audio: HardwareState["audio"] = null;
  try {
    audio = (await callJsonRpc(page, "getAudioConfig")) as HardwareState["audio"];
  } catch (error) {
    if (!/method not found/i.test(String(error))) throw error;
  }
  const media = (await callJsonRpc(page, "getVirtualMediaState")) as MediaState | null;
  return { config, devices, enabled, audio, media };
}

export async function restoreMedia(page: Page, media: MediaState | null): Promise<void> {
  if (JSON.stringify(await callJsonRpc(page, "getVirtualMediaState")) === JSON.stringify(media))
    return;
  await callJsonRpc(page, "unmountImage");
  if (media) {
    if (media.source === "HTTP" && media.url) {
      await callJsonRpc(page, "mountWithHTTP", { url: media.url, mode: media.mode });
    } else if (media.filename) {
      await callJsonRpc(page, "mountWithStorage", { filename: media.filename, mode: media.mode });
    } else {
      throw new Error(`Cannot restore media source ${media.source}`);
    }
  }
  expect(await callJsonRpc(page, "getVirtualMediaState")).toEqual(media);
}

export async function configureTestUSB(
  page: Page,
  state: HardwareState,
  audio = false,
): Promise<void> {
  // Establish a recognizable host identity even when the user selected a
  // custom VID/PID or disabled an input class before the test.
  if (state.audio?.enabled)
    await callJsonRpc(page, "setAudioConfig", { params: { enabled: false } });
  if (state.media) await callJsonRpc(page, "unmountImage");
  await callJsonRpc(page, "setUsbEmulationState", { enabled: true });
  const config = {
    vendor_id: "0x1d6b",
    product_id: "0x0104",
    serial_number: state.config.serial_number,
    manufacturer: "JetKVM",
    product: "USB Emulation Device",
  };
  await callJsonRpc(page, "setUsbConfig", { usbConfig: config });
  await callJsonRpc(page, "setUsbDevices", {
    devices: {
      ...state.devices,
      keyboard: true,
      absolute_mouse: true,
      relative_mouse: true,
      mass_storage: true,
      ...(state.audio ? { audio } : {}),
    },
  });
  await ensureRpcReady(page);
  expect(await callJsonRpc(page, "getUsbConfig")).toMatchObject(config);
}

export async function restoreHardwareState(page: Page, state: HardwareState): Promise<void> {
  await ensureRpcReady(page, { navigateFirst: true });
  const errors: unknown[] = [];
  const step = async (action: () => Promise<unknown>) => {
    try {
      await action();
    } catch (error) {
      errors.push(error);
    }
  };
  // Attempt every independent restoration even if one operation fails.
  if (state.audio)
    await step(() => callJsonRpc(page, "setAudioConfig", { params: { enabled: false } }));
  await step(() => callJsonRpc(page, "unmountImage"));
  await step(() => callJsonRpc(page, "setUsbDevices", { devices: state.devices }));
  await step(() => callJsonRpc(page, "setUsbConfig", { usbConfig: state.config }));
  await step(() => restoreMedia(page, state.media));
  if (state.audio) await step(() => callJsonRpc(page, "setAudioConfig", { params: state.audio! }));
  await step(() => callJsonRpc(page, "setUsbEmulationState", { enabled: state.enabled }));
  await step(async () => expect(await captureHardwareState(page)).toEqual(state));
  if (errors.length) throw new AggregateError(errors, "Hardware state restoration failed");
}
