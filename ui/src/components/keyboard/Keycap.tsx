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
  const { x, y, w, h, shape, legends, scancode, dead, homing, decal, color, textColor } =
    transportKey;

  const widthClass = getWidthClass(w);
  const isCustomWidth = widthClass === "w-custom";

  // `shape` is already the correct CSS class, no client-side shape detection needed.
  // A key is a "letter" if its normal legend is a single Unicode letter (any script).
  // Used by CSS to apply CapsLock layer switching (shift legend for letters only).
  const isLetter = legends.normal != null && /^\p{Ll}$/u.test(legends.normal);

  const className = [
    "key",
    widthClass,
    shape, // '' | 'iso-enter' | 'big-ass-enter' | 'stepped-caps'
    dead && "dead",
    homing && "homing",
    decal && "decal",
    isPressed && "pressed",
    isLetter && "letter",
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
      {legends.normal && (
        <span className="legend normal" aria-hidden="true">
          {legends.normal}
        </span>
      )}
      {legends.shift && (
        <span className="legend shift" aria-hidden="true">
          {legends.shift}
        </span>
      )}
      {legends.altgr && (
        <span className="legend altgr" aria-hidden="true">
          {legends.altgr}
        </span>
      )}
      {legends.shiftAltgr && (
        <span className="legend shift-altgr" aria-hidden="true">
          {legends.shiftAltgr}
        </span>
      )}
      {legends.kana && (
        <span className="legend kana" aria-hidden="true">
          {legends.kana}
        </span>
      )}
      {legends.shiftKana && (
        <span className="legend shift-kana" aria-hidden="true">
          {legends.shiftKana}
        </span>
      )}
    </div>
  );
});

/**
 * Maps legend strings (symbols and abbreviations) to localized accessible names.
 * Covers the unicode symbols we use for special keys plus common abbreviations
 * from KLE files that screen readers would not pronounce correctly.
 */
const KEY_ARIA_NAMES: Record<string, () => string> = {
  // Unicode symbols (used by built-in layouts)
  "⌦": () => m.keys_delete(),
  "⌫": () => m.keys_backspace(),
  "↵": () => m.keys_enter(),
  "⏎": () => m.keys_enter(),
  "⇥": () => m.keys_tab(),
  "⇪": () => m.keys_caps_lock(),
  "↑": () => m.keys_arrow_up(),
  "↓": () => m.keys_arrow_down(),
  "←": () => m.keys_arrow_left(),
  "→": () => m.keys_arrow_right(),
  "⌥": () => m.keys_alt(),
  "⌥ Alt": () => m.keys_alt(),
  "⌃": () => m.keys_control(),
  "⌃ Ctrl": () => m.keys_control(),
  "⇧": () => m.keys_shift(),
  "⇧ Shift": () => m.keys_shift(),
  "⌘": () => m.keys_meta(),
  "⌘ Meta": () => m.keys_meta(),
  "☰": () => m.keys_menu(),
  "☰ Menu": () => m.keys_menu(),
  "⊞": () => m.keys_meta(),
  // Spelled-out equivalents (for user-uploaded KLE files)
  Backspace: () => m.keys_backspace(),
  Enter: () => m.keys_enter(),
  Tab: () => m.keys_tab(),
  "Caps Lock": () => m.keys_caps_lock(),
  Caps: () => m.keys_caps_lock(),
  "Arrow Up": () => m.keys_arrow_up(),
  "Arrow Down": () => m.keys_arrow_down(),
  "Arrow Left": () => m.keys_arrow_left(),
  "Arrow Right": () => m.keys_arrow_right(),
  Up: () => m.keys_arrow_up(),
  Down: () => m.keys_arrow_down(),
  Left: () => m.keys_arrow_left(),
  Right: () => m.keys_arrow_right(),
  // Abbreviations
  Esc: () => m.keys_escape(),
  Escape: () => m.keys_escape(),
  Space: () => m.keys_space(),
  Ins: () => m.keys_insert(),
  Insert: () => m.keys_insert(),
  Del: () => m.keys_delete(),
  Delete: () => m.keys_delete(),
  Home: () => m.keys_home(),
  End: () => m.keys_end(),
  PgUp: () => m.keys_page_up(),
  "Page Up": () => m.keys_page_up(),
  PgDn: () => m.keys_page_down(),
  "Page Down": () => m.keys_page_down(),
  PrtSc: () => m.keys_print_screen(),
  "Print Screen": () => m.keys_print_screen(),
  ScrLk: () => m.keys_scroll_lock(),
  "Scroll Lock": () => m.keys_scroll_lock(),
  NumLk: () => m.keys_num_lock(),
  "Num Lock": () => m.keys_num_lock(),
  Pause: () => m.keys_pause(),
  // Modifiers
  LCtrl: () => m.keys_control(),
  RCtrl: () => m.keys_control(),
  Ctrl: () => m.keys_control(),
  Control: () => m.keys_control(),
  LShift: () => m.keys_shift(),
  RShift: () => m.keys_shift(),
  Shift: () => m.keys_shift(),
  LAlt: () => m.keys_alt(),
  RAlt: () => m.keys_alt(),
  Alt: () => m.keys_alt(),
  AltGr: () => m.keys_altgr(),
  LWin: () => m.keys_meta(),
  RWin: () => m.keys_meta(),
  Win: () => m.keys_meta(),
  Super: () => m.keys_meta(),
  Meta: () => m.keys_meta(),
  Menu: () => m.keys_menu(),
  App: () => m.keys_menu(),
  Windows: () => m.keys_meta(),
  Command: () => m.keys_meta(),
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
