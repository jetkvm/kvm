import { useEffect, useLayoutEffect, useRef } from "react";
import { Terminal } from "xterm";
import { Unicode11Addon } from "@xterm/addon-unicode11";
import { WebglAddon } from "@xterm/addon-webgl";
import { WebLinksAddon } from "@xterm/addon-web-links";
import { FitAddon } from "@xterm/addon-fit";
import { ClipboardAddon } from "@xterm/addon-clipboard";

import "xterm/css/xterm.css";
import { useRTCStore, useUiStore } from "../hooks/stores";

const isWebGl2Supported = !!document.createElement("canvas").getContext("webgl2");

// Add this debounce function at the top of the file
function debounce(func: (...args: any[]) => void, wait: number) {
  let timeout: number | null = null;
  return (...args: any[]) => {
    if (timeout) clearTimeout(timeout);
    timeout = setTimeout(() => func(...args), wait);
  };
}

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
  // Add these configurations:
  convertEol: true,
  linuxMode: false, // Disable Linux mode which might affect line endings
} as const;

interface XTermProps {
  terminalChannel: RTCDataChannel | null;
}

export function XTerm({ terminalChannel }: XTermProps) {
  const xtermRef = useRef<Terminal | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const terminalElmRef = useRef<HTMLDivElement | null>(null);
  const fitAddonRef = useRef<FitAddon | null>(null);
  const setEnableTerminal = useUiStore(state => state.setEnableTerminal);
  const setDisableKeyboardFocusTrap = useUiStore(state => state.setDisableVideoFocusTrap);
  const peerConnection = useRTCStore(state => state.peerConnection);

  useEffect(() => {
    setDisableKeyboardFocusTrap(true);

    return () => {
      setDisableKeyboardFocusTrap(false);
    };
  }, [setDisableKeyboardFocusTrap]);

  const initializeTerminalAddons = (term: Terminal) => {
    const fitAddon = new FitAddon();
    term.loadAddon(fitAddon);
    term.loadAddon(new ClipboardAddon());
    term.loadAddon(new Unicode11Addon());
    term.loadAddon(new WebLinksAddon());
    term.unicode.activeVersion = "11";

    if (isWebGl2Supported) {
      const webGl2Addon = new WebglAddon();
      webGl2Addon.onContextLoss(() => webGl2Addon.dispose());
      term.loadAddon(webGl2Addon);
    }

    return fitAddon;
  };

  const setupTerminalChannel = (
    term: Terminal,
    channel: RTCDataChannel,
    abortController: AbortController,
  ) => {
    channel.onopen = () => {
      // Handle terminal input
      term.onData(data => {
        if (channel.readyState === "open") {
          channel.send(data);
        }
      });

      // Handle terminal output
      channel.addEventListener(
        "message",
        (event: MessageEvent) => {
          term.write(new Uint8Array(event.data));
        },
        { signal: abortController.signal },
      );

      // Send initial terminal size
      if (channel.readyState === "open") {
        channel.send(JSON.stringify({ rows: term.rows, cols: term.cols }));
      }
    };
  };

  useLayoutEffect(() => {
    if (!terminalElmRef.current) return;

    // Ensure the container has dimensions before initializing
    if (!terminalElmRef.current.offsetHeight || !terminalElmRef.current.offsetWidth) {
      return;
    }

    const term = new Terminal(TERMINAL_CONFIG);
    const fitAddon = initializeTerminalAddons(term);
    const abortController = new AbortController();

    // Setup escape key handler
    term.onKey(e => {
      const { domEvent } = e;
      if (domEvent.key === "Escape") {
        setEnableTerminal(false);
        setDisableKeyboardFocusTrap(false);
        domEvent.preventDefault();
      }
    });

    let elm: HTMLDivElement | null = terminalElmRef.current;
    // Initialize terminal
    setTimeout(() => {
      if (elm) {
        console.log("opening terminal");
        term.open(elm);
        fitAddon.fit();
      }
    }, 800);

    xtermRef.current = term;
    fitAddonRef.current = fitAddon;

    // Setup resize handling
    const debouncedResizeHandler = debounce(() => fitAddon.fit(), 100);
    const resizeObserver = new ResizeObserver(debouncedResizeHandler);
    resizeObserver.observe(terminalElmRef.current);

    // Focus terminal after a short delay
    setTimeout(() => {
      term.focus();
      terminalElmRef.current?.focus();
    }, 500);

    // Setup terminal channel if available
    const channel = peerConnection?.createDataChannel("terminal");
    if (channel) {
      setupTerminalChannel(term, channel, abortController);
    }

    // Cleanup
    return () => {
      resizeObserver.disconnect();
      abortController.abort();
      term.dispose();
      elm = null;
      xtermRef.current = null;
      fitAddonRef.current = null;
    };
  }, [peerConnection, setDisableKeyboardFocusTrap, setEnableTerminal, terminalChannel]);

  return (
    <div className="w-full h-full" ref={containerRef}>
      <div
        className="w-full h-full terminal-container"
        ref={terminalElmRef}
        style={{ display: "flex", minHeight: "100%" }}
      ></div>
    </div>
  );
}
