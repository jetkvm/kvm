import { useCallback, useEffect, useState } from "react";
import { useXTerm } from "react-xtermjs";
import { FitAddon } from "@xterm/addon-fit";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import { ClipboardAddon } from "@xterm/addon-clipboard";
import { LuRefreshCw } from "react-icons/lu";

import { m } from "@localizations/messages.js";
import { Button } from "@components/Button";

// Standalone serial console for the extension port UART, served when the
// Serial Console extension's network access is set to "Web console". It
// talks to /serial/ws, which relays raw bytes both ways, so unlike the
// in-session console there is no line normalisation or RX/TX labelling.

const TERMINAL_OPTIONS = {
  theme: { background: "#0f172a", foreground: "#e2e8f0", cursor: "#93a1a1" },
  fontFamily: "'Fira Code', Menlo, Monaco, 'Courier New', monospace",
  fontSize: 14,
  allowProposedApi: true,
  scrollback: 10000,
  cursorBlink: true,
  macOptionIsMeta: true,
} as const;

type ConnectionState =
  | { kind: "connecting" }
  | { kind: "connected" }
  | { kind: "disconnected"; reason: string };

function serialSocketURL() {
  const proto = window.location.protocol === "https:" ? "wss:" : "ws:";
  return `${proto}//${window.location.host}/serial/ws`;
}

export default function SerialWebConsoleRoute() {
  const { instance, ref } = useXTerm({ options: TERMINAL_OPTIONS });
  const [state, setState] = useState<ConnectionState>({ kind: "connecting" });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    document.title = `${m.serial_web_console_title()} - JetKVM`;
  }, []);

  useEffect(() => {
    if (!instance) return;
    const fit = new FitAddon();
    instance.loadAddon(fit);
    instance.loadAddon(new ClipboardAddon());
    instance.loadAddon(new Unicode11Addon());
    instance.loadAddon(new WebLinksAddon());
    fit.fit();
    instance.focus();

    const onResize = () => fit.fit();
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, [instance]);

  useEffect(() => {
    if (!instance) return;

    setState({ kind: "connecting" });
    const ws = new WebSocket(serialSocketURL());
    ws.binaryType = "arraybuffer";
    const encoder = new TextEncoder();

    ws.onopen = () => setState({ kind: "connected" });
    ws.onmessage = e => {
      if (typeof e.data === "string") instance.write(e.data);
      else instance.write(new Uint8Array(e.data as ArrayBuffer));
    };
    ws.onclose = e => {
      setState({ kind: "disconnected", reason: e.reason || `code ${e.code}` });
    };

    const onData = instance.onData(data => {
      if (ws.readyState === WebSocket.OPEN) ws.send(encoder.encode(data));
    });
    // Binary input (e.g. some mouse reports) is Latin-1 encoded by xterm.
    const onBinary = instance.onBinary(data => {
      if (ws.readyState !== WebSocket.OPEN) return;
      ws.send(Uint8Array.from(data, c => c.charCodeAt(0) & 0xff));
    });

    return () => {
      onData.dispose();
      onBinary.dispose();
      ws.onclose = null;
      ws.close();
    };
  }, [instance, attempt]);

  const reconnect = useCallback(() => setAttempt(a => a + 1), []);

  const status =
    state.kind === "connecting"
      ? m.serial_web_console_connecting()
      : state.kind === "connected"
        ? m.serial_web_console_connected()
        : m.serial_web_console_disconnected({ reason: state.reason });

  return (
    <div className="flex h-screen flex-col bg-[#0f172a]">
      <div className="flex items-center justify-between border-b border-slate-700 bg-slate-800 px-3 py-2">
        <h1 className="text-sm font-medium text-slate-200">{m.serial_web_console_title()}</h1>
        <div className="flex items-center gap-3">
          <span
            className={
              state.kind === "connected"
                ? "text-xs text-green-400"
                : state.kind === "connecting"
                  ? "text-xs text-slate-400"
                  : "text-xs text-red-400"
            }
          >
            {status}
          </span>
          {state.kind === "disconnected" && (
            <Button
              size="XS"
              theme="light"
              LeadingIcon={LuRefreshCw}
              text={m.serial_web_console_reconnect()}
              onClick={reconnect}
            />
          )}
        </div>
      </div>
      <div className="min-h-0 flex-1 p-2">
        <div ref={ref} className="h-full w-full" />
      </div>
    </div>
  );
}
