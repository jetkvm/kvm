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
import { isControlScancode } from "../../keyboardMappings";
import { m } from "@localizations/messages.js";

// Shared key-alias taxonomy with the Go backend (which embeds the same file
// at internal/keyboard/keyaliases.json). Each (canonical + alias) string is
// mapped to m.keys_<ariaKey>() via ARIA_KEY_TO_FN below — adding a new alias
// means editing only the JSON; adding a new logical key means editing both
// the JSON and ARIA_KEY_TO_FN.
import keyAliases from "../../../../internal/keyboard/keyaliases.json";

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

  // A key is a "letter" if its normal legend is a single Unicode letter (any script).
  // Used by CSS to apply CapsLock layer switching (shift legend for letters only).
  const isLetter = legends.normal != null && /^\p{Ll}$/u.test(legends.normal);
  const isMetaControl = isControlScancode(scancode);

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

  const renderLegend = (text: string | undefined, type: string, normalDupsShift = false) => {
    if (!text) return null;
    const deadClass = isDeadLegend(type) ? "dead" : "";
    const dupsClass = normalDupsShift ? "nds" : "";
    return (
      <span className={`legend ${type} ${deadClass} ${dupsClass}`.trim()} aria-hidden="true">
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
      {renderLegend(legends.normal, "normal", legends.normal === legends.shift)}
      {renderLegend(legends.shift, "shift", legends.shift === legends.normal)}
      {renderLegend(legends.altgr, "altgr")}
      {renderLegend(legends.shiftAltgr, "shift-altgr")}
      {renderLegend(legends.kana, "kana")}
      {renderLegend(legends.shiftKana, "shift-kana")}
    </div>
  );
});

// One entry per logical key. Compile-time validation that every ariaKey used
// in keyaliases.json corresponds to a real localization message.
const ARIA_KEY_TO_FN: Record<string, () => string> = {
  alt: m.keys_alt,
  altgr: m.keys_altgr,
  application: m.keys_application,
  arrow_down: m.keys_arrow_down,
  arrow_left: m.keys_arrow_left,
  arrow_right: m.keys_arrow_right,
  arrow_up: m.keys_arrow_up,
  backspace: m.keys_backspace,
  caps_lock: m.keys_caps_lock,
  command: m.keys_command,
  control: m.keys_control,
  delete: m.keys_delete,
  end: m.keys_end,
  enter: m.keys_enter,
  escape: m.keys_escape,
  home: m.keys_home,
  insert: m.keys_insert,
  menu: m.keys_menu,
  meta: m.keys_meta,
  num_lock: m.keys_num_lock,
  option: m.keys_option,
  page_down: m.keys_page_down,
  page_up: m.keys_page_up,
  pause: m.keys_pause,
  print_screen: m.keys_print_screen,
  scroll_lock: m.keys_scroll_lock,
  shift: m.keys_shift,
  space: m.keys_space,
  tab: m.keys_tab,
};

const KEY_ARIA_NAMES: Record<string, () => string> = (() => {
  const map: Record<string, () => string> = {};
  for (const sk of keyAliases.specialKeys) {
    const fn = ARIA_KEY_TO_FN[sk.ariaKey];
    if (!fn) {
      throw new Error(
        `keyAliases.json: ariaKey "${sk.ariaKey}" has no localization in ARIA_KEY_TO_FN`,
      );
    }
    map[sk.canonical] = fn;
    for (const alias of sk.aliases) {
      map[alias] = fn;
    }
  }
  return map;
})();

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
    parts.push(`${m.keys_modifier_shift()}: ${resolveKeyName(legends.shift)}`);
  }
  if (legends.altgr) {
    parts.push(`${m.keys_modifier_altgr()}: ${resolveKeyName(legends.altgr)}`);
  }
  if (legends.shiftAltgr) {
    parts.push(`${m.keys_modifier_altgr_shift()}: ${resolveKeyName(legends.shiftAltgr)}`);
  }
  if (legends.kana) {
    parts.push(`${m.keys_modifier_kana()}: ${resolveKeyName(legends.kana)}`);
  }
  if (legends.shiftKana) {
    parts.push(`${m.keys_modifier_kana_shift()}: ${resolveKeyName(legends.shiftKana)}`);
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
