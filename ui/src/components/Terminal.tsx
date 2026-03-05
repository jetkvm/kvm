import "react-simple-keyboard/build/css/index.css";
import { ChevronDownIcon, PauseCircleIcon, PlayCircleIcon } from "@heroicons/react/16/solid";
import { useEffect, useMemo, useCallback, useState } from "react";
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
}: {
  readonly title: string;
  readonly dataChannel: RTCDataChannel;
  readonly type: AvailableTerminalTypes;
}) {
  const { terminalType, setTerminalType, setDisableVideoFocusTrap } = useUiStore();
  const { terminator } = useTerminalStore();
  const { instance, ref } = useXTerm({ options: TERMINAL_CONFIG });
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
    dataChannel.addEventListener(
      "message",
      e => {
        if (typeof e.data === "string") {
          instance.write(e.data); // text path
          return;
        }
        // binary path (if the server ever sends bytes)
        // Handle binary data differently based on browser implementation
        // Firefox sends data as blobs, chrome sends data as arraybuffer
        if (binaryType === "arraybuffer") {
          instance.write(new Uint8Array(e.data));
        } else if (binaryType === "blob") {
          const reader = new FileReader();
          reader.onload = () => {
            if (!reader.result) return;
            instance.write(new Uint8Array(reader.result as ArrayBuffer));
          };
          reader.readAsArrayBuffer(e.data);
        }
      },
      { signal: abortController.signal },
    );

    const onDataHandler = instance.onData(data => {
      if (data === "\r") {
        // Intercept enter key to add terminator
        dataChannel.send(terminator ?? "");
      } else {
        dataChannel.send(
          JSON.stringify({
            type: "serial",
            data, // string
          }),
        );
      }
    });

    // Setup escape key handler
    const onKeyHandler = instance.onKey(e => {
      const { domEvent } = e;
      if (domEvent.key === "Escape") {
        setTerminalType("none");
        setDisableVideoFocusTrap(false);
        domEvent.preventDefault();
      }
    });

    // Send initial terminal size
    if (dataChannel.readyState === "open") {
      dataChannel.send(
        JSON.stringify({
          type: "system",
          name: "term.size",
          data: { rows: instance.rows, cols: instance.cols },
        }),
      );
    }

    return () => {
      abortController.abort();
      onDataHandler.dispose();
      onKeyHandler.dispose();
    };
  }, [dataChannel, instance, readyState, setDisableVideoFocusTrap, setTerminalType, terminator]);

  useEffect(() => {
    if (!instance) return;

    // Load the fit addon
    const fitAddon = new FitAddon();
    instance.loadAddon(fitAddon);

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
    };
  }, [instance]);

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
            <div className="flex items-center justify-center border-y border-y-slate-800/30 bg-white px-2 py-1 dark:border-y-slate-300/20 dark:bg-slate-800">
              <h2 className="self-center font-sans text-[12px] text-slate-700 select-none dark:text-slate-300">
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
