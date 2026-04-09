/**
 * transport/schema.ts
 *
 * TypeScript definitions for the KeyboardLayout transport type.
 *
 * These types define what the Go backend sends and the React client receives.
 * The client does NO parsing and NO scancode inference — it only renders
 * and dispatches scancodes from this pre-processed structure.
 *
 * ⚠️  JSON field names here are the wire contract with the Go backend.
 *     Do not rename fields without updating go/keyboard.go simultaneously.
 *
 * All keyboard types (transport and client-side) live in this file.
 */

// ---------------------------------------------------------------------------
// Active layer state (client-only, not part of transport)
// ---------------------------------------------------------------------------

/**
 * The active display layer of the virtual keyboard.
 * - 'normal'      : base layer (no modifiers)
 * - 'shift'       : Shift held
 * - 'altgr'       : AltGr (Right Alt) held
 * - 'shift-altgr' : Shift + AltGr held
 * - 'all'         : preview mode — shows all layers simultaneously in quadrants
 *
 * This value is set as data-layer="..." on the .vkb container.
 * All legend show/hide logic is handled entirely by CSS selectors on this attribute.
 */
export type KeyLayer = 'normal' | 'shift' | 'altgr' | 'shift-altgr' | 'all';

// ---------------------------------------------------------------------------
// HID modifier constants
// ---------------------------------------------------------------------------

export const MOD_NONE: number   = 0x00;
export const MOD_LCTRL: number  = 0x01;
export const MOD_LSHIFT: number  = 0x02;
export const MOD_LALT: number   = 0x04;
export const MOD_LMETA: number   = 0x08;
export const MOD_RCTRL: number  = 0x10;
export const MOD_RSHIFT: number = 0x20;
export const MOD_RALT: number  = 0x40;
export const MOD_RMETA: number  = 0x80;

export const MOD_ALTGR: number  = MOD_RALT; // Right Alt
export const MOD_SHIFT_ALTGR: number = MOD_LSHIFT | MOD_ALTGR; // Shift + AltGr

// ---------------------------------------------------------------------------
// HIDCombo — a single key event to send over USB HID
// ---------------------------------------------------------------------------

/**
 * A USB HID key event: scancode + modifier byte.
 * Short field names because charMap can have 200+ entries and is sent
 * on every session connect.
 */
export interface HIDCombo {
  /** USB HID Usage ID (page 0x07), e.g. 0x04 = A, 0x1E = 1 */
  s: number;
  /** Modifier byte: 0=none, 0x02=Shift, 0x40=AltGr, 0x42=Shift+AltGr */
  m: number;
  /** Dead key prefix — if present, send this HID event first, then the main key.
   *  Used for composed characters (e.g. ^ then a → â) and standalone dead keys
   *  (e.g. ^ then Space → ^). */
  p?: HIDCombo;
}

// ---------------------------------------------------------------------------
// Legend data
// ---------------------------------------------------------------------------

/**
 * The four legend slots on a keycap, corresponding to modifier combinations.
 * Derived from the KLE legend string by splitting on '\n' and reading positions.
 *
 * KLE legend position mapping (default alignment a=4):
 *   position 0 → normal      (unshifted)
 *   position 1 → shift       (Shift)
 *   position 2 → altgr       (AltGr / Right Alt)
 *   position 3 → shiftAltgr  (Shift + AltGr)
 *
 * Example KLE string "1\n!\n²\n¹" produces:
 *   { normal: '1', shift: '!', altgr: '²', shiftAltgr: '¹' }
 */
export interface KeyLegends {
  normal?:     string;
  shift?:      string;
  altgr?:      string;
  shiftAltgr?: string;
}

// ---------------------------------------------------------------------------
// Key shape
// ---------------------------------------------------------------------------

/**
 * Pre-computed shape class for the keycap div.
 * Computed by Go from the KLE w/h/w2/h2 signature.
 * Applied directly as a CSS class — no client-side shape detection needed.
 */
export type KeyShape =
  | ''             // standard rectangle
  | 'iso-enter'
  | 'big-ass-enter'
  | 'stepped-caps';

// ---------------------------------------------------------------------------
// TransportKey — a single keycap, fully resolved
// ---------------------------------------------------------------------------

/**
 * A single keycap ready for rendering and HID dispatch.
 * All derived values (shape, scancode, dead) are pre-computed by Go.
 */
export interface TransportKey {
  // --- Position and size (in keyboard units) ---
  x: number;
  y: number;
  w: number;   // width,  default 1
  h: number;   // height, default 1

  // Second rectangle for L-shaped keys (ISO enter, stepped caps).
  // Omitted entirely for standard rectangular keys.
  w2?: number;
  h2?: number;
  x2?: number;
  y2?: number;

  // --- Pre-computed by Go ---

  /** CSS class to apply to the keycap div. Empty string for normal keys. */
  shape: KeyShape;

  /** The four legend layers. Fields omitted when no legend for that layer. */
  legends: KeyLegends;

  /**
   * USB HID Usage ID for this key's physical position.
   * 0 = non-typeable key (modifier-only, unknown).
   * This is the value sent in the HID report when the virtual key is pressed.
   */
  scancode: number;

  /**
   * True if any legend on this key is a dead key character.
   * (^, ~, ¨, `, ´, ¸ etc.)
   * CSS class 'dead' is added to the keycap when true.
   */
  dead: boolean;

  /** Homing key — has a tactile bump or bar (typically F and J keys). */
  homing: boolean;

  /** Decal — a label printed on the keyboard, not a physical keycap. */
  decal: boolean;

  // --- Optional KLE colorway (only present when KLE file specifies colors) ---
  color?:     string;  // CSS color string, e.g. "#2d2d2d"
  textColor?: string;  // CSS color string, e.g. "#e0e0e0"
}

// ---------------------------------------------------------------------------
// KeyboardLayout — the full transport type
// ---------------------------------------------------------------------------

/**
 * A fully processed keyboard layout, ready to pass to <VirtualKeyboard />.
 *
 * Produced by Go on upload, stored as JSON, served via JSON-RPC.
 * The React client treats this as opaque render data.
 */
export interface KeyboardLayout {
  /** Unique identifier. Built-in layouts use locale codes (e.g. "de_DE").
   *  User-uploaded layouts use UUIDs. */
  id: string;

  /** Display name for settings UI (from KLE meta.name or user-provided). */
  name: string;

  /** Author (from KLE meta.author). Optional. */
  author?: string;

  /** Total board width in keyboard units. Used for --board-w CSS variable. */
  boardW: number;

  /** Total board height in keyboard units. Used for --board-h CSS variable. */
  boardH: number;

  /** All keycaps in the layout. Order is row-major (top-left to bottom-right). */
  keys: TransportKey[];

  /**
   * Character-to-HID map for the Paste Text system.
   *
   * Maps a Unicode character (single codepoint) to the HID scancode +
   * modifier combination that produces it on the target machine.
   *
   * Built by Go from the key legends. First occurrence of each character wins.
   *
   * TODO!
   *  Dead key compositions (e.g. ^+e → ê) are NOT in this map — they require
   * the optional dead key sequence list (future work).
   *
   * Example:
   *   { "a": {"s": 4, "m": 0}, "A": {"s": 4, "m": 2}, "@": {"s": 31, "m": 64} }
   */
  charMap: Record<string, HIDCombo>;
}

// ---------------------------------------------------------------------------
// LayoutMeta — lightweight listing type for the settings dropdown
// ---------------------------------------------------------------------------

/**
 * Returned by rpcGetKeyboardLayouts().
 * Does NOT include keys or charMap — just enough to populate a picker.
 */
export interface LayoutMeta {
  id:      string;
  name:    string;
  builtin: boolean;
}

// ---------------------------------------------------------------------------
// Upload response
// ---------------------------------------------------------------------------

/**
 * Returned by POST /keyboard/upload on success.
 */
export interface LayoutUploadResponse {
  id:        string;    // UUID assigned to the new layout
  name:      string;    // display name (from KLE meta or ?name param)
  keyCount:  number;    // number of keys parsed (for user confirmation)
  warnings?: string[];  // non-fatal issues found during parsing
}
