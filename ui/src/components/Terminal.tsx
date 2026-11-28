import "react-simple-keyboard/build/css/index.css";
import { ChevronDownIcon, PauseCircleIcon, PlayCircleIcon } from "@heroicons/react/16/solid";
import { MutableRefObject, useEffect, useMemo, useCallback, useRef, useState } from "react";
import { useXTerm } from "react-xtermjs";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { WebglAddon } from "@xterm/addon-webgl";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import { ClipboardAddon } from "@xterm/addon-clipboard";

import { m } from "@localizations/messages.js";
import { cx } from "@/cva.config";
import { AvailableTerminalTypes, useUiStore, useTerminalStore } from "@/hooks/stores";
import { CommandInput } from "@/components/CommandInput";
import { JsonRpcResponse, useJsonRpc } from "@/hooks/useJsonRpc";
import notifications from "@/notifications";
import { Button } from "@components/Button";

const isWebGl2Supported = !!document.createElement("canvas").getContext("webgl2");

// Guest terminal keystrokes go out as binary; a paste is split so no single
// message exceeds what a data channel accepts.
const guestInputEncoder = new TextEncoder();
const guestInputChunk = 16 * 1024;

// Terminal theme configuration
const SOLARIZED_THEME = {
  background: "#0f172a", // Solarized base03
  foreground: "#839496", // Solarized base0
  cursor: "#93a1a1", // Solarized base1
  cursorAccent: "#002b36", // Solarized base03
  black: "#073642", // Solarized base02
  red: "#dc322f", // Solarized red
  green: "#859900", // Solarized green
  yellow: "#b58900", // Solarized yellow
  blue: "#268bd2", // Solarized blue
  magenta: "#d33682", // Solarized magenta
  cyan: "#2aa198", // Solarized cyan
  white: "#eee8d5", // Solarized base2
  brightBlack: "#002b36", // Solarized base03
  brightRed: "#cb4b16", // Solarized orange
  brightGreen: "#586e75", // Solarized base01
  brightYellow: "#657b83", // Solarized base00
  brightBlue: "#839496", // Solarized base0
  brightMagenta: "#6c71c4", // Solarized violet
  brightCyan: "#93a1a1", // Solarized base1
  brightWhite: "#fdf6e3", // Solarized base3
} as const;

const TERMINAL_CONFIG = {
  theme: SOLARIZED_THEME,
  fontFamily: "'Fira Code', Menlo, Monaco, 'Courier New', monospace",
  fontSize: 13,
  allowProposedApi: true,
  scrollback: 1000,
  cursorBlink: true,
  smoothScrollDuration: 100,
  macOptionIsMeta: true,
  macOptionClickForcesSelection: true,
  convertEol: true,
  linuxMode: false,
  // Add these configurations:
  cursorStyle: "block",
  rendererType: "canvas", // Ensure we're using the canvas renderer
  unicode: { activeVersion: "11" },
} as const;

function Terminal({
  title,
  dataChannel,
  type,
  backlog,
}: {
  readonly title: string;
  readonly dataChannel: RTCDataChannel;
  readonly type: AvailableTerminalTypes;
  // Messages the channel received before this terminal was listening.
  readonly backlog?: MutableRefObject<(string | ArrayBuffer | Blob)[]>;
}) {
  const { terminalType, setTerminalType, setDisableVideoFocusTrap, disableVideoFocusTrap } =
    useUiStore();
  const { terminator } = useTerminalStore();
  const { instance, ref } = useXTerm({ options: TERMINAL_CONFIG });
  const fitAddonRef = useRef<FitAddon | null>(null);
  const [terminalPaused, setTerminalPaused] = useState(false);

  const isTerminalTypeEnabled = useMemo(() => {
    console.log("Terminal type:", terminalType, "Checking against:", type);
    return terminalType == type;
  }, [terminalType, type]);

  useEffect(() => {
    setTimeout(() => {
      setDisableVideoFocusTrap(isTerminalTypeEnabled);
    }, 500);

    return () => {
      setDisableVideoFocusTrap(false);
    };
  }, [setDisableVideoFocusTrap, isTerminalTypeEnabled]);

  // Re-focus xterm when the focus trap is released back to the terminal
  // (e.g. after closing the Settings modal while a terminal is open)
  useEffect(() => {
    if (disableVideoFocusTrap && isTerminalTypeEnabled && instance) {
      instance.focus();
    }
  }, [disableVideoFocusTrap, isTerminalTypeEnabled, instance]);

  const readyState = dataChannel.readyState;

  const { send } = useJsonRpc();

  const handleTerminalPauseChange = () => {
    send("setTerminalPaused", { terminalPaused: !terminalPaused }, (resp: JsonRpcResponse) => {
      if ("error" in resp) {
        notifications.error(
          `Failed to update terminal pause state: ${resp.error.data || "Unknown error"}`,
        );
        return;
      }
      setTerminalPaused(!terminalPaused);
    });
  };
  useEffect(() => {
    if (!instance) return;
    if (readyState !== "open") return;

    const abortController = new AbortController();
    const binaryType = dataChannel.binaryType;
    const deliver = (data: string | ArrayBuffer | Blob) => {
      if (typeof data === "string") {
        instance.write(data); // text path
        return;
      }
      // binary path (if the server ever sends bytes)
      // Handle binary data differently based on browser implementation
      // Firefox sends data as blobs, chrome sends data as arraybuffer
      if (binaryType === "arraybuffer") {
        instance.write(new Uint8Array(data as ArrayBuffer));
      } else if (binaryType === "blob") {
        const reader = new FileReader();
        reader.onload = () => {
          if (!reader.result) return;
          instance.write(new Uint8Array(reader.result as ArrayBuffer));
        };
        reader.readAsArrayBuffer(data as Blob);
      }
    };
    dataChannel.addEventListener("message", e => deliver(e.data), {
      signal: abortController.signal,
    });
    if (backlog) {
      // The listener now sees every message; hand it what arrived earlier.
      dataChannel.onmessage = null;
      backlog.current.splice(0).forEach(deliver);
    }

    const onDataHandler = instance.onData(data => {
      if (type === "guest") {
        // The guest terminal takes keystrokes as binary, so its text
        // messages can only ever be the size below and pasted JSON is never
        // mistaken for one. A channel can close under a still-mounted
        // panel, and keystrokes then have nowhere to go.
        if (dataChannel.readyState !== "open") return;
        const bytes = guestInputEncoder.encode(data);
        for (let offset = 0; offset < bytes.length; offset += guestInputChunk) {
          dataChannel.send(bytes.subarray(offset, offset + guestInputChunk));
        }
      } else if (type === "kvm" || type === "cdcacm") {
        dataChannel.send(data);
      } else {
        if (data === "\r") {
          dataChannel.send(terminator ?? "");
        } else {
          dataChannel.send(
            JSON.stringify({
              type: "serial",
              data,
            }),
          );
        }
      }
    });

    // Setup escape key handler. A guest shell takes Escape itself (vi, less);
    // its panel hides with the Hide button.
    const onKeyHandler = instance.onKey(e => {
      const { domEvent } = e;
      if (domEvent.key === "Escape" && type !== "guest") {
        setTerminalType("none");
        setDisableVideoFocusTrap(false);
        domEvent.preventDefault();
      }
    });

    // The guest lays its shell's screen out for the panel's size, so it
    // learns the size again whenever the panel is refitted.
    const onResizeHandler = instance.onResize(({ rows, cols }) => {
      if (type !== "guest" || dataChannel.readyState !== "open") return;
      dataChannel.send(JSON.stringify({ rows, cols }));
    });

    // Send initial terminal size (not applicable for CDC-ACM raw serial)
    if (dataChannel.readyState === "open" && type !== "cdcacm") {
      if (type === "kvm" || type === "guest") {
        dataChannel.send(JSON.stringify({ rows: instance.rows, cols: instance.cols }));
      } else {
        dataChannel.send(
          JSON.stringify({
            type: "system",
            name: "term.size",
            data: { rows: instance.rows, cols: instance.cols },
          }),
        );
      }
    }

    return () => {
      abortController.abort();
      onDataHandler.dispose();
      onKeyHandler.dispose();
      onResizeHandler.dispose();
    };
  }, [
    backlog,
    dataChannel,
    instance,
    readyState,
    setDisableVideoFocusTrap,
    setTerminalType,
    terminator,
    type,
  ]);

  useEffect(() => {
    if (!instance) return;

    // Load the fit addon
    const fitAddon = new FitAddon();
    instance.loadAddon(fitAddon);
    fitAddonRef.current = fitAddon;

    instance.loadAddon(new ClipboardAddon());
    instance.loadAddon(new Unicode11Addon());
    instance.loadAddon(new WebLinksAddon());

    if (isWebGl2Supported) {
      const webGl2Addon = new WebglAddon();
      webGl2Addon.onContextLoss(() => webGl2Addon.dispose());
      instance.loadAddon(webGl2Addon);
    }

    const handleResize = () => fitAddon.fit();

    // Handle resize event
    window.addEventListener("resize", handleResize);
    return () => {
      window.removeEventListener("resize", handleResize);
      fitAddonRef.current = null;
    };
  }, [instance]);

  // The guest terminal fills its panel from the moment it appears, not only
  // after a window resize, since the guest's shell is sized to it.
  useEffect(() => {
    if (type === "guest" && isTerminalTypeEnabled) fitAddonRef.current?.fit();
  }, [instance, isTerminalTypeEnabled, type]);

  const sendLine = useCallback(
    (line: string) => {
      // Just send; line ending/echo/normalization handled in serial.go
      dataChannel.send(line + terminator);
    },
    [dataChannel, terminator],
  );

  return (
    <div onKeyDown={e => e.stopPropagation()} onKeyUp={e => e.stopPropagation()}>
      <div>
        <div
          className={cx(
            [
              // Base styles
              "fixed bottom-0 w-full transform transition duration-500 ease-in-out",
              "translate-y-0",
            ],
            {
              "pointer-events-none translate-y-[500px] opacity-100 transition duration-300":
                !isTerminalTypeEnabled,
              "pointer-events-auto translate-y-0 opacity-100 transition duration-300":
                isTerminalTypeEnabled,
            },
          )}
        >
          <div className="h-[500px] w-full bg-[#0f172a]">
            <div className="flex items-center justify-center border-y border-y-slate-800/30 bg-white px-2 py-4 dark:border-y-slate-300/20 dark:bg-slate-800">
              <h2 className="self-center font-sans text-sm leading-none font-medium text-slate-700 select-none dark:text-slate-300">
                {title}
              </h2>
              <div className="absolute right-2">
                {terminalType == "serial" && (
                  <Button
                    size="XS"
                    theme="light"
                    text={terminalPaused ? "Resume" : "Pause"}
                    LeadingIcon={terminalPaused ? PlayCircleIcon : PauseCircleIcon}
                    onClick={() => {
                      handleTerminalPauseChange();
                    }}
                    data-testid={undefined}
                  />
                )}
                <Button
                  size="XS"
                  theme="light"
                  text={m.hide()}
                  LeadingIcon={ChevronDownIcon}
                  onClick={() => setTerminalType("none")}
                  data-testid={undefined}
                />
              </div>
            </div>

            <div className="h-[calc(100%-36px)] p-3">
              <div
                key="serial"
                ref={ref}
                style={{ height: terminalType === "serial" ? "90%" : "100%", width: "100%" }}
              />
              {terminalType == "serial" && (
                <CommandInput
                  placeholder="Type serial command…  (Enter to send • ↑/↓ history • Ctrl+R search)"
                  onSend={sendLine}
                  className="mt-2"
                />
              )}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}

export default Terminal;
