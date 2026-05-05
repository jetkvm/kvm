# KLE Transport Schema

## Overview

KLE JSON files are parsed **on the Go backend** (the KVM device). The React client receives a single processed `KeyboardLayout` JSON object via JSON-RPC and uses it directly for rendering and HID dispatch. The client does zero parsing and zero scancode inference.

## Flow

```text
User uploads .json (KLE format)
        │
        ▼
POST /keyboard/upload   (raw JSON body, requires auth)
        │
        ▼
Go: ParseKLE(rawJSON)       internal/keyboard/keyboard.go
        │
        ├─→ validate
        ├─→ build []TransportKey  (positions, legends, scancodes, shape)
        ├─→ build CharMap         (char → scancode+modifiers, for paste)
        └─→ addDeadKeyCompositions (Unicode NFC → prefixed HIDCombos)
        │
        ▼
Store as /userdata/kvm_layouts/<id>.layout.json
        │
        ▼
JSON-RPC: getKeyboardLayoutData(id) → KeyboardLayout (transport type)
        │
        ▼
React: <VirtualKeyboard layout={layout} />   (render only)
       PasteModal uses layout.charMap         (paste with dead key support)
       Macro UI uses buildKeyDisplayMap()     (key display names)
```

Built-in layouts (19 KLE files) are embedded in the binary via `go:embed`
and served through the same `getKeyboardLayoutData` RPC. Layout IDs use
hyphens (`en-US`, `de-DE`) matching existing device config values; the
file lookup converts to underscores (`en_US.kle.json`).

### KLE Metadata Extensions

The optional metadata object (first element of the KLE array) supports two
JetKVM-specific fields in addition to the standard KLE `name`, `author`, etc.:

| Field | Type | Default | Purpose |
|---|---|---|---|
| `deadKeys` | `string[]` | `[]` (none) | Legend characters that are dead keys. Gates **both** the `dead: true` flag on `TransportKey` (CSS `.dead` class) **and** charMap composition generation via Unicode NFC. Layouts without this field produce no dead key compositions — characters like `^` and `~` are treated as normal keys that output directly. |
| `scancodes` | `Record<string, number>` | `{}` (none) | Maps key index (0-based, parse order) to a USB HID Usage ID, overriding position-based inference. Use for non-standard layouts where keys are in unusual positions. |

**Example** (German QWERTZ with dead keys and a hypothetical scancode override):

```json
[
  {
    "name": "Deutsch de-DE (ISO 105)",
    "author": "JetKVM",
    "deadKeys": ["^", "´", "`"],
    "scancodes": { "42": 76 }
  },
  ["Esc", {"x": 1}, "F1", "F2", "..."],
  ...
]
```

In this example, keys whose normal legend is `^`, `´`, or `` ` `` get the dead
key visual indicator, and key #42 (0-based) is forced to scancode 76 (Delete)
regardless of its physical position.

## Why Parse on the Go Side

1. **Single parse:** layout is parsed once on upload, stored as the processed
   format, served cheaply on every client connect.
2. **Testable in isolation:** The KLE parser has comprehensive table-driven
   tests covering edge cases (ISO enter, stepped caps, dead key compositions,
   auto-case, all built-in layouts).
3. **Validation owns errors:** parse errors surface at upload time with clear
   messages, not silently at render time in the browser.
4. **The client is a renderer:** React only needs to know *what to draw* and
   *what scancode to send*. It should not know *how* KLE encodes key widths.

## Transport Type: `KeyboardLayout`

Defined in full in `ui/src/components/keyboard/types/schema.ts` (TypeScript)
and `internal/keyboard/keyboard.go` (Go). The schema file contains both
transport types and the client-side `KeyLayer` type. Both sides must stay in
sync — the JSON field names are the contract.

### Top Level

```jsonc
{
  "id":       "de-DE",           // unique identifier (built-in) or UUID (uploaded)
  "name":     "German (QWERTZ)", // display name, from KLE meta or user-provided
  "author":   "JetKVM",          // from KLE meta.author, optional
  "boardW":   22.0,              // total width in keyboard units
  "boardH":   6.0,               // total height in keyboard units
  "keys":     [ /* []TransportKey */ ],
  "charMap":  { /* Record<string, HIDCombo> */ }
}
```

### `TransportKey`

```jsonc
{
  // Position & size (in keyboard units, from KLE)
  "x":  0.0,
  "y":  1.0,
  "w":  1.0,      // width,  default 1
  "h":  1.0,      // height, default 1
  "w2": 1.5,      // second rect width  (ISO enter etc.), omitted if absent
  "h2": 1.0,      // second rect height, omitted if absent
  "x2": -0.25,    // second rect x offset, omitted if absent
  "y2": 0.0,      // second rect y offset, omitted if absent

  // Shape class — computed by Go, consumed directly as CSS class by React.
  // One of: "" | "iso-enter" | "big-ass-enter" | "stepped-caps"
  "shape": "iso-enter",

  // Legends — only present layers included; absent = key has no legend for that layer.
  // The Go parser auto-generates case pairs for single-letter keys (e.g. "Q" → normal: "q", shift: "Q").
  "legends": {
    "normal":     "1",
    "shift":      "!",
    "altgr":      "²",
    "shiftAltgr": "¹"
  },

  // USB HID Usage ID (0x07 page). 0 = non-typeable (modifier, etc.)
  "scancode": 30,

  // Whether this key is a dead key, driven by metadata `deadKeys` array,
  // not character detection. True only if the normal legend matches a
  // declared dead key in the KLE metadata.
  "dead": false,

  // Whether this key has a homing bump (KLE "n" property)
  "homing": false,

  // Whether this key is a decal / non-functional label (KLE "d" property)
  "decal": false,

  // Control-like classification — see "Scancode classification" below.
  "controlLike": false,

  // KLE colorway (optional — only present if KLE file specifies per-key colors)
  "color":     "#2d2d2d",
  "textColor": "#e0e0e0"
}
```

#### Scancode classification (`controlLike`)

The Go backend is the single source of truth for whether a scancode is
"control-like" (modifier, navigation, function key, …) versus
text-producing (letters, digits, punctuation, the ISO key, printable
numpad keys). Two helpers in `internal/keyboard/scancode.go`:

- `ScancodeProducesText(sc)` — true for keys that, when pressed, type a
  character. Excludes Enter, Escape, Backspace, Tab, NumLock, KPEnter
  even though their HID usage IDs sit inside the printable ranges.
- `IsControlScancode(sc)` — the inverse plus an explicit list of
  "looks-like-text-but-treat-as-control" keys (notably Space, which
  produces a character but should still take the meta-control CSS
  class on a keycap).

`ParseKLE` evaluates `IsControlScancode(Scancode)` once per key and
stamps the result onto `TransportKey.controlLike`. The frontend reads
this field directly and does **not** maintain its own classifier — any
classification logic must be added on the Go side, where it can be unit
tested (`TestScancodeClassificationContract` in `keyboard_test.go`).

### `HIDCombo` (values in `charMap`)

```jsonc
{
  "s": 30,    // scancode (USB HID Usage ID)
  "m": 0,     // modifiers byte: 0=none, 2=Shift, 64=AltGr, 66=Shift+AltGr
  "p": {      // dead key prefix (optional — only for composed/dead key characters)
    "s": 47,  //   send this key first (the dead key)
    "m": 0    //   with these modifiers
  }
}
```

Short field names (`s`, `m`, `p`) are intentional — `charMap` can have 200+
entries and the layout is served on every session connect.

For simple characters, `p` is omitted. For dead key compositions:

- `"â"` → `{s: 4, m: 0, p: {s: 47, m: 0}}` — press `^` (dead), then `a`
- `"^"` → `{s: 0x2C, m: 0, p: {s: 47, m: 0}}` — press `^` (dead), then Space

### Modifier byte constants (same as HID spec)

| Constant        | Value | Meaning        |
|-----------------|-------|----------------|
| MOD_NONE        | 0x00  | No modifier    |
| MOD_LSHIFT      | 0x02  | Left Shift     |
| MOD_ALTGR       | 0x40  | Right Alt      |
| MOD_SHIFT_ALTGR | 0x42  | Shift + AltGr  |

## JSON-RPC Methods

### `getKeyboardLayouts() → []LayoutMeta`

Returns the list of available layouts (built-in + uploaded). Does not include
the full key data — just enough to populate the settings dropdown.

```jsonc
[
  { "id": "en-US", "name": "English (US)",      "builtin": true  },
  { "id": "de-DE", "name": "German (QWERTZ)",   "builtin": true  },
  { "id": "fr-FR", "name": "French (AZERTY)",   "builtin": true  },
  { "id": "uuid-abc123", "name": "My Layout",   "builtin": false }
]
```

### `getKeyboardLayoutData(id: string) → KeyboardLayout`

Returns the full transport type for the given layout ID.
Called once when the session starts (or when the user changes layout in settings).

Note: the existing `getKeyboardLayout` RPC (no params) returns the active layout
ID string from config. `getKeyboardLayoutData` returns the full layout content.

### `deleteKeyboardLayout(id: string) → void`

Deletes a user-uploaded layout. Returns an error if `id` is a built-in layout.

## HTTP Upload Endpoint

KLE files can be large-ish raw JSON. Upload via HTTP rather than JSON-RPC
to avoid base64 overhead and to get streaming parse. This endpoint requires
authentication (it is behind the `protected` route group in `web.go`).

```text
POST /keyboard/upload
Content-Type: application/json   (raw KLE JSON body)

Query params:
  ?name=My+Layout    optional display name override

Response 200:
  {
    "id": "uuid-abc123",
    "name": "My Layout",
    "keyCount": 87,
    "warnings": ["3 of 87 keys have no HID scancode (97% coverage)..."]
  }

  The `warnings` array is omitted when empty. Warnings are non-fatal — the
  layout is stored and usable, but the user should review the issues (e.g.
  unmapped keys, low charMap coverage, unsupported form factor).

Response 400:
  { "error": "KLE parse error: row 3 contains invalid property object" }
```

The upload endpoint calls the same `ParseKLE()` function internally and
stores the resulting `KeyboardLayout` JSON to `/userdata/kvm_layouts/`.
