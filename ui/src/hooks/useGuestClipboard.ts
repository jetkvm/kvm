import { useCallback } from "react";

import { useCapability, useRTCStore, useUiStore, useVideoStore } from "@hooks/stores";
import useKeyboard from "@hooks/useKeyboard";
import { useCopyToClipboard } from "@components/useCopyToClipBoard";
import notifications from "@/notifications";
import { isMac, sleep } from "@/utils";
import { callJsonRpc } from "@/utils/jsonrpc";
import { m } from "@localizations/messages.js";

// Wire limit of the OS Services clipboard service, in UTF-8 bytes.
export const guestClipboardMaxBytes = 65528;

// The JetKVM Mini refuses a guestClipboardSet request of 160 KiB or more,
// and no device gets a larger one; 256 bytes are left for the request around
// the text. JSON escapes a control character to six bytes, so text within
// the wire limit can still be too large.
const guestClipboardMaxJsonBytes = 160 * 1024 - 256;

function fitsGuestClipboard(text: string) {
  const encoder = new TextEncoder();
  return (
    encoder.encode(text).length <= guestClipboardMaxBytes &&
    encoder.encode(JSON.stringify(text)).length <= guestClipboardMaxJsonBytes
  );
}

// The guest has to receive the copy keystroke and update its own clipboard
// before that clipboard can be read back.
const guestCopySettleMs = 250;

// Delay of the guest OS copy and paste shortcuts, as for typed text.
const shortcutDelay = 20;

// The device answers a clipboard request within 4.5 s of starting it and a
// file drop within 2.5 s, and one request can wait ahead of it in the
// device's guest queue: 9 s, and a second for the network.
const guestRpcTimeoutMs = 10000;

type GuestClipboardStatus = "ok" | "unsupported" | "empty" | "denied" | "failed";

interface GuestClipboardContent {
  status: GuestClipboardStatus;
  text: string;
  truncated: boolean;
}

export type PasteResult = "pasted" | "unavailable" | "failed";

export function clipboardShortcut(key: "V" | "C") {
  return isMac() ? `⌘⇧${key}` : `Ctrl+Shift+${key}`;
}

function statusMessage(status: GuestClipboardStatus) {
  switch (status) {
    case "empty":
      return m.clipboard_guest_empty();
    case "denied":
      return m.clipboard_guest_denied();
    case "unsupported":
      return m.clipboard_guest_unsupported();
    default:
      return m.clipboard_guest_failed();
  }
}

// A gesture's request goes out now or fails: callJsonRpc would wait for a
// reconnect and paste long after the keystroke.
export async function guestRpc<T>(method: string, params: object): Promise<T> {
  if (useRTCStore.getState().rpcDataChannel?.readyState !== "open") {
    throw new Error("not connected to the device");
  }
  const response = await callJsonRpc<T>({
    method,
    params,
    attemptTimeoutMs: guestRpcTimeoutMs,
  });
  // The device puts the reason in data, as useJsonRpc's JsonRpcError has it.
  if (response.error) throw new Error((response.error.data as string) || response.error.message);
  return response.result;
}

// Every transfer presses keys on the guest, so two running at once (a held
// chord auto-repeating, a double click on Confirm) would interleave their
// keystrokes. The video surface and the paste popover each hold a hook
// instance, so the flag lives at module scope and later gestures are dropped
// until the running one completes.
let transferring = false;

async function exclusive<T>(fallback: T, run: () => Promise<T>): Promise<T> {
  if (transferring) return fallback;
  transferring = true;
  try {
    return await run();
  } finally {
    transferring = false;
  }
}

// Clipboard transfer through the OS Services sender of a device with OS
// Services, with the browser-only paths (typing keystrokes, screen text
// recognition) as fallbacks. On any other device there is no guest
// clipboard and every gesture takes its fallback.
export default function useGuestClipboard() {
  const osServices = useCapability("os_services");
  const {
    guestClipboard: reportedClipboard,
    setGuestClipboard,
    width: videoWidth,
    height: videoHeight,
  } = useVideoStore();
  const guestClipboard = osServices ? reportedClipboard : null;
  const { setOcrMode } = useUiStore();
  const { executeMacro, resetKeyboardState } = useKeyboard();
  const { copy } = useCopyToClipboard();

  // The guest advertised a clipboard it no longer has. Withdrawing the
  // capability sends this and every later gesture down the fallback path
  // until the next session announces it again.
  const withdraw = useCallback(() => setGuestClipboard(null), [setGuestClipboard]);

  // Puts text on the guest clipboard and presses the guest OS paste shortcut.
  const pasteViaGuest = useCallback(
    (text: string): Promise<PasteResult> => {
      if (!guestClipboard?.write) return Promise.resolve("unavailable");
      return exclusive<PasteResult>("failed", async () => {
        if (!fitsGuestClipboard(text)) {
          notifications.error(m.clipboard_too_large());
          return "failed";
        }
        try {
          const { status } = await guestRpc<{ status: GuestClipboardStatus }>("guestClipboardSet", {
            text,
          });
          if (status === "unsupported") {
            withdraw();
            return "unavailable";
          }
          if (status !== "ok") {
            notifications.error(statusMessage(status));
            return "failed";
          }
          // The chord that triggered this left Ctrl/Shift held on the guest.
          await resetKeyboardState();
          await executeMacro([
            {
              keys: ["KeyV"],
              modifiers: [guestClipboard.os === "macos" ? "MetaLeft" : "ControlLeft"],
              delay: shortcutDelay,
            },
          ]);
          return "pasted";
        } catch (error) {
          notifications.error(m.clipboard_guest_error({ error: String(error) }));
          return "failed";
        }
      });
    },
    [guestClipboard, withdraw, resetKeyboardState, executeMacro],
  );

  const recognizeScreenText = useCallback(() => {
    if (videoWidth > 0 && videoHeight > 0) setOcrMode(true);
  }, [videoWidth, videoHeight, setOcrMode]);

  // Copies the guest's selection into the browser's clipboard. The guest is
  // told to copy first, mirroring the paste direction: the gesture means "copy
  // what is selected over there", not "read whatever the guest clipboard
  // happens to hold". The read must still complete inside the user gesture's
  // activation window for the clipboard write to be allowed on plain-http
  // origins. Without an OS Services clipboard this opens screen text
  // recognition instead.
  const copyFromGuest = useCallback((): Promise<void> => {
    if (!guestClipboard?.read) {
      recognizeScreenText();
      return Promise.resolve();
    }
    return exclusive(undefined, async () => {
      try {
        // The chord that triggered this left modifiers held on the guest.
        await resetKeyboardState();
        await executeMacro([
          {
            keys: ["KeyC"],
            modifiers: [guestClipboard.os === "macos" ? "MetaLeft" : "ControlLeft"],
            delay: shortcutDelay,
          },
        ]);
        await sleep(guestCopySettleMs);
        const content = await guestRpc<GuestClipboardContent>("guestClipboardGet", {});
        if (content.status === "unsupported") {
          withdraw();
          recognizeScreenText();
          return;
        }
        if (content.status !== "ok") {
          notifications.error(statusMessage(content.status));
          return;
        }
        if (!(await copy(content.text))) {
          notifications.error(m.clipboard_copy_blocked());
          return;
        }
        if (content.truncated) notifications.error(m.clipboard_guest_truncated());
        else notifications.success(m.clipboard_copied_from_guest());
      } catch (error) {
        notifications.error(m.clipboard_guest_error({ error: String(error) }));
      }
    });
  }, [guestClipboard, recognizeScreenText, withdraw, copy, resetKeyboardState, executeMacro]);

  return { osServices, guestClipboard, pasteViaGuest, copyFromGuest };
}
