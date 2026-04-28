/**
 * A single keycap. Consumes TransportKey from the Go backend directly.
 *
 * The `shape` field is a pre-computed CSS class name ('' | 'iso-enter' |
 * 'big-ass-enter' | 'stepped-caps') applied directly to the div.
 *
 * Layer visibility is CSS-only via data-layer on the parent .vkb element.
 * React.memo ensures layer changes do NOT rerender any Keycap instance.
 */

import React, { memo, useCallback } from "react";
import { TransportKey, KeyLegends } from "./types/schema";
import { m } from "@localizations/messages.js";

// ---------------------------------------------------------------------------
// Props
// ---------------------------------------------------------------------------

export interface KeycapProps {
  /** Pre-processed key data from the Go backend via JSON-RPC. */
  transportKey: TransportKey;

  /** Called on pointerdown with the HID scancode to send. */
  onPress: (scancode: number, legends: KeyLegends) => void;

  /** Visual pressed state — controlled externally from keyboard state tracker. */
  isPressed?: boolean;
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

export const Keycap = memo(function Keycap({ transportKey, onPress, isPressed }: KeycapProps) {
  const { x, y, w, h, shape, legends, scancode, deadLegends, homing, decal, color, textColor } =
    transportKey;

  const widthClass = getWidthClass(w);
  const isCustomWidth = widthClass === "w-custom";

  // `shape` is already the correct CSS class, no client-side shape detection needed.
  // A key is a "letter" if its normal legend is a single Unicode letter (any script).
  // Used by CSS to apply CapsLock layer switching (shift legend for letters only).
  const isLetter = legends.normal != null && /^\p{Ll}$/u.test(legends.normal);
  const isMetaControl = META_CONTROL_SCANCODES.has(scancode);

  const className = [
    "key",
    widthClass,
    shape, // '' | 'iso-enter' | 'big-ass-enter' | 'stepped-caps'
    homing && "homing",
    decal && "decal",
    isPressed && "pressed",
    isLetter && "letter",
    isMetaControl && "meta-control",
  ]
    .filter(Boolean)
    .join(" ");

  // For ISO Enter (shape with x2/w2), position at x+x2 so the wider top part aligns correctly
  const visualX = transportKey.x2 ? x + (transportKey.x2 ?? 0) : x;

  const inlineStyle: React.CSSProperties = {
    "--kx": visualX,
    "--ky": y,
    ...(h !== 1 && { "--kh": h }),
    ...(color && { "--key-color": color }),
    ...(textColor && { "--key-text-color": textColor }),
    ...(isCustomWidth && getCustomWidthStyle(w)),
  } as React.CSSProperties;

  const handlePointerDown = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      // Prevent focus steal from the main video/session element.
      e.preventDefault();
      if (scancode !== 0) {
        onPress(scancode, legends);
      }
    },
    [scancode, legends, onPress],
  );

  const isDeadLegend = (legendType: string): boolean => {
    return deadLegends != null && deadLegends.includes(legendType);
  };

  const renderLegend = (text: string | undefined, type: string, displayClass: string) => {
    if (!text) return null;
    const deadClass = isDeadLegend(type) ? "dead" : "";
    return (
      <span className={`legend ${displayClass} ${deadClass}`} aria-hidden="true">
        {text}
      </span>
    );
  };

  return (
    <div
      className={className}
      style={inlineStyle}
      data-scancode={scancode || undefined}
      onPointerDown={handlePointerDown}
      aria-label={ariaLabel(legends)}
      role="button"
      tabIndex={
        -1
      } /* intentionally unfocusable — physical keyboard must always reach the KVM session */
    >
      {renderLegend(legends.normal, "normal", "normal")}
      {renderLegend(legends.shift, "shift", "shift")}
      {renderLegend(legends.altgr, "altgr", "altgr")}
      {renderLegend(legends.shiftAltgr, "shift-altgr", "shift-altgr")}
      {renderLegend(legends.kana, "kana", "kana")}
      {renderLegend(legends.shiftKana, "shift-kana", "shift-kana")}
    </div>
  );
});

/**
 * Maps legend strings (symbols and abbreviations) to localized accessible names.
 * Covers the unicode symbols we use for special keys plus common abbreviations
 * from KLE files that screen readers would not pronounce correctly.
 */
const KEY_ARIA_NAMES: Record<string, () => string> = {
  "⇮": () => m.keys_alt(), // up-down arrow U21EE
  "⌥": () => m.keys_alt(), // alternative key symbol U2387
  "⌥ Alt": () => m.keys_alt(),
  Alt: () => m.keys_alt(),
  LAlt: () => m.keys_alt(),
  RAlt: () => m.keys_alt(),
  AltGr: () => m.keys_altgr(),

  App: () => m.keys_application(),
  Application: () => m.keys_application(),

  "↓": () => m.keys_arrow_down(), // downwards arrow U2193
  "▼": () => m.keys_arrow_down(), // black down-pointing triangle U25B7
  Down: () => m.keys_arrow_down(),
  ArrowDown: () => m.keys_arrow_down(),
  "Arrow Down": () => m.keys_arrow_down(),

  "←": () => m.keys_arrow_left(), // leftwards arrow U2190
  "◀": () => m.keys_arrow_left(), // black left-pointing triangle U25C0
  Left: () => m.keys_arrow_left(),
  ArrowLeft: () => m.keys_arrow_left(),
  "Arrow Left": () => m.keys_arrow_left(),

  "→": () => m.keys_arrow_right(), // rightwards arrow U2192
  "▶": () => m.keys_arrow_right(), // black right-pointing triangle U25B6
  Right: () => m.keys_arrow_right(),
  ArrowRight: () => m.keys_arrow_right(),
  "Arrow Right": () => m.keys_arrow_right(),

  "↑": () => m.keys_arrow_up(), // upwards arrow U2191
  "▲": () => m.keys_arrow_up(), // black up-pointing triangle U25B2
  Up: () => m.keys_arrow_up(),
  ArrowUp: () => m.keys_arrow_up(),
  "Arrow Up": () => m.keys_arrow_up(),

  "⌫": () => m.keys_backspace(), // erase to the left symbol U27F5
  "⟵": () => m.keys_backspace(), // long leftwards arrow U27F5 sometimes used on Apple layouts
  "␈": () => m.keys_backspace(),
  BS: () => m.keys_backspace(),
  Backspace: () => m.keys_backspace(),

  "⇪": () => m.keys_caps_lock(), // upward arrow with horizontal bar U21EA
  "🄰": () => m.keys_caps_lock(), // squared latin capital A
  "🅰": () => m.keys_caps_lock(), // negative squared latin capital A
  "⇬": () => m.keys_caps_lock(), // up arrow with double horizontal bar U21AC sometimes used on Candian layouts
  Caps: () => m.keys_caps_lock(),
  CapsLock: () => m.keys_caps_lock(),
  "Caps Lock": () => m.keys_caps_lock(),

  "⌘": () => m.keys_command(), // place of interest symbol U2318 sometimes used on Mac layouts
  "⌘ Meta": () => m.keys_command(),
  Command: () => m.keys_command(),

  "⌃": () => m.keys_control(), // upward arrowhead symbol U2303 sometimes used for Ctrl
  "⌃ Ctrl": () => m.keys_control(),
  "✲": () => m.keys_control(), // open-center-asterisk symbol
  "⎈": () => m.keys_control(), // helm symbol sometimes used on Mac layouts
  LCtrl: () => m.keys_control(),
  RCtrl: () => m.keys_control(),
  Ctrl: () => m.keys_control(),
  Control: () => m.keys_control(),

  "⌦": () => m.keys_delete(),
  Del: () => m.keys_delete(),
  Delete: () => m.keys_delete(),

  "⤓": () => m.keys_end(), // downwards arrow to bar U2913
  "⇥": () => m.keys_end(), // rightwards arrow to bar U21E5
  End: () => m.keys_end(),

  "↵": () => m.keys_enter(), // downwards arrow U21B5
  "⏎": () => m.keys_enter(), // return symbol U23CE
  "↩": () => m.keys_enter(), // downwards arrow with corner leftwards U21A9
  "⮠.": () => m.keys_enter(), // downwards triangle-headed arrow with long tip leftwards U2B20 sometimes used on Japanese layouts
  "⌤": () => m.keys_enter(), // up arrowhead between two horizontal bars U2324 sometimes used for number keypad Enter on Apple layouts
  "⎆": () => m.keys_enter(), // enter symbol U2386
  "␍": () => m.keys_enter(),
  CR: () => m.keys_enter(),
  Enter: () => m.keys_enter(),

  "⎋": () => m.keys_escape(), // broken circle with northwest arrow U238B sometimes used on Apple layouts
  "␛": () => m.keys_escape(),
  Esc: () => m.keys_escape(),
  Escape: () => m.keys_escape(),

  "⤒": () => m.keys_home(), // upwards arrow to bar U2912
  "⇤": () => m.keys_home(), // leftwards arrow to bar U21E4
  Home: () => m.keys_home(),

  "⎀": () => m.keys_insert(), // insert symbol U2380
  Ins: () => m.keys_insert(),
  Insert: () => m.keys_insert(),

  "☰": () => m.keys_menu(), // trigram for heaven symbol U2630
  "☰ Menu": () => m.keys_menu(),
  "▤": () => m.keys_menu(), // square with horizontal fill symbol U25A4
  "▤ Menu": () => m.keys_menu(),
  Menu: () => m.keys_menu(),

  "❖": () => m.keys_meta(), // black diamond minus white X symbol
  "◆": () => m.keys_meta(), // black diamond symbol U25C6
  "⊞": () => m.keys_meta(), // squared plus symbol U229E
  LWin: () => m.keys_meta(),
  RWin: () => m.keys_meta(),
  Win: () => m.keys_meta(),
  Super: () => m.keys_meta(),
  Meta: () => m.keys_meta(),
  Windows: () => m.keys_meta(),

  "⇭": () => m.keys_num_lock(), // upwards white arrow on pedestal with vertical bar U21ED
  NumLk: () => m.keys_num_lock(),
  "Num Lock": () => m.keys_num_lock(),

  Option: () => m.keys_option(), // common Mac label for Alt

  "⇟": () => m.keys_page_down(), // downwards arrow with double stroke U21DF
  PgDn: () => m.keys_page_down(),
  PageDown: () => m.keys_page_down(),
  "Page Down": () => m.keys_page_down(),

  "⇞": () => m.keys_page_up(), // upwards arrow with double stroke U21DE
  PgUp: () => m.keys_page_up(),
  PageUp: () => m.keys_page_up(),
  "Page Up": () => m.keys_page_up(),

  Pause: () => m.keys_pause(),

  "⎙": () => m.keys_print_screen(), // print screen symbol U2399
  PrtSc: () => m.keys_print_screen(),
  PrintScreen: () => m.keys_print_screen(),
  "Print Screen": () => m.keys_print_screen(),

  "⇳": () => m.keys_scroll_lock(), // up down white arrow U21F3
  ScrLk: () => m.keys_scroll_lock(),
  ScrollLock: () => m.keys_scroll_lock(),
  "Scroll Lock": () => m.keys_scroll_lock(),

  "⇧": () => m.keys_shift(), // upwards white arrow U21E7
  "⇧ Shift": () => m.keys_shift(),
  "Shift ⇧": () => m.keys_shift(),
  Shift: () => m.keys_shift(),
  LShift: () => m.keys_shift(),
  RShift: () => m.keys_shift(),

  " ": () => m.keys_space(),
  "␣": () => m.keys_space(),
  "␠": () => m.keys_space(),
  SP: () => m.keys_space(),
  Space: () => m.keys_space(),

  "⭾": () => m.keys_tab(), // horizontal tab key U2B7E
  "↹": () => m.keys_tab(), // leftwards arrow to bar over rightwards arrow to bar U21B9
  "⇄": () => m.keys_tab(), // rightwards arrow over leftwards arrow U21E4
  "␉": () => m.keys_tab(),
  HT: () => m.keys_tab(),
  Tab: () => m.keys_tab(),
};

function resolveKeyName(legend: string): string {
  const lookup = KEY_ARIA_NAMES[legend];
  return lookup ? lookup() : legend;
}

function ariaLabel(legends: KeyLegends): string {
  const parts: string[] = [];
  if (legends.normal) {
    parts.push(resolveKeyName(legends.normal));
  }
  if (legends.shift && legends.shift !== legends.normal?.toUpperCase()) {
    parts.push(`Shift: ${resolveKeyName(legends.shift)}`);
  }
  if (legends.altgr) {
    parts.push(`AltGr: ${resolveKeyName(legends.altgr)}`);
  }
  if (legends.shiftAltgr) {
    parts.push(`Shift+AltGr: ${resolveKeyName(legends.shiftAltgr)}`);
  }
  if (legends.kana) {
    parts.push(`Kana: ${resolveKeyName(legends.kana)}`);
  }
  if (legends.shiftKana) {
    parts.push(`Shift+Kana: ${resolveKeyName(legends.shiftKana)}`);
  }

  return parts.join(", ") || "key";
}

// Keys whose base label should remain visible in AltGr preview layers.
const META_CONTROL_SCANCODES = new Set<number>([
  40, // Enter
  41, // Esc
  42, // Backspace
  43, // Tab
  57, // CapsLock
  101, // Menu/Application
  224, // Left Ctrl
  225, // Left Shift
  226, // Left Alt
  227, // Left Meta
  228, // Right Ctrl
  229, // Right Shift
  230, // Right Alt (AltGr)
  231, // Right Meta
]);

/**
 * Maps KLE width values to CSS class names.
 * Values are rounded to 2 decimal places before lookup to handle float drift.
 */
const WIDTH_CLASS_MAP: Record<number, string> = {
  // Standard ANSI/ISO widths
  100: "", // default — no class needed, CSS default applies
  125: "w-125",
  150: "w-150",
  175: "w-175",
  200: "w-200",
  225: "w-225",
  250: "w-250",
  275: "w-275",
  300: "w-300",
  350: "w-350", // JIS spacebar
  400: "w-400", // decal / wide keys
  625: "w-625", // standard spacebar

  // Less common
  600: "w-600", // some 60% spacebars
  700: "w-700", // some WKL spacebars
};

/**
 * Returns the CSS width class for a given KLE width value.
 * Falls back to a data attribute if the width is not in the standard table,
 * allowing a CSS custom property fallback in the stylesheet.
 */
export function getWidthClass(w: number): string {
  const rounded = Math.round(w * 100);
  const cls = WIDTH_CLASS_MAP[rounded];
  if (cls !== undefined) return cls;
  // Non-standard width — caller should also set --key-w CSS variable inline
  return "w-custom";
}

/**
 * Given a key with a non-standard width, returns the inline style
 * object to set the --key-w custom property so the CSS can size it correctly.
 *
 * Usage: if getWidthClass() returns 'w-custom', also spread this onto the element's style.
 */
export function getCustomWidthStyle(w: number): React.CSSProperties {
  return { "--key-w": w } as React.CSSProperties;
}
