# JetKVM Virtual Keyboard — Development Guide

> **Purpose:** Reference for engineers working on the keyboard subsystem internals (parser, charMap, scancode tables).
>
> **Adding a new layout?** See **[ADDING_A_LAYOUT.md](ADDING_A_LAYOUT.md)** — a step-by-step contributor walkthrough. The summary below is just a reminder of the moving parts.

---

## Adding a Layout (Quick Reference)

For the full step-by-step guide, see **[ADDING_A_LAYOUT.md](ADDING_A_LAYOUT.md)**.

Quick summary:

1. Drop a new `<locale>.kle.json` into `internal/keyboard/layouts/`. Filename uses underscores (`de_DE`); the layout ID uses hyphens (`de-DE`) — converted automatically.
2. First element of the JSON is the metadata block (`name`, `author`, optional `deadKeys`, optional `kbdLayoutInfo`).
3. Auto-discovered via `go:embed`; no registration code to write. Aliases live in `layoutAliases` in `builtin.go`.
4. Validate with `go test ./internal/keyboard/...` and `go run ./scripts/audit_layouts.go <locale>`.

---

## Scancode Overrides for Non-Standard Layouts

For layouts where keys are in non-standard physical positions (split keyboards, ortholinear, custom PCBs), position-based scancode inference may produce incorrect results. The `scancodes` metadata field provides per-key overrides:

```json
{
  "name": "My Custom Split",
  "scancodes": { "42": 76, "55": 83 }
}
```

The key index is 0-based in parse order (the order keys appear in the KLE JSON, left-to-right, top-to-bottom). The value is the USB HID Usage ID (decimal).

To find the correct key index:

1. Open the KLE JSON and count keys from the beginning (starting at 0)
2. Property objects (width, gap, etc.) do not count as keys
3. Only legend strings increment the key counter

Common HID Usage IDs for overrides:

| Key | HID Usage ID (decimal) | HID Usage ID (hex) |
|---|---|---|
| Delete | 76 | 0x4C |
| Insert | 73 | 0x49 |
| Home | 74 | 0x4A |
| End | 77 | 0x4D |
| Page Up | 75 | 0x4B |
| Page Down | 78 | 0x4E |
| Num Lock | 83 | 0x53 |
| Print Screen | 70 | 0x46 |

---

## Compact Layout Support

The parser supports **75% and TKL** compact form factors using a separate
position table without the y:0.5 gap. Selection criteria:

- **Full-size:** `boardW > 20` or `keyCount >= 100`
- **Compact (75%/TKL):** `boardW <= 20`, `keyCount < 100`, `boardH >= 6`
- **60%/65%:** Falls back to full-size table (most keys get `scancode=0`).
  These layouts require `scancodes` metadata overrides for every key.

If the compact table maps a few edge-case keys incorrectly, use `scancodes`
metadata overrides to fix them rather than modifying the table (which would
affect all compact layouts).

---

## Dead Key Auditing

When adding a layout with dead keys, verify the `deadKeys` metadata against the operating system's actual dead key behavior:

1. Open the OS keyboard settings for the target layout
2. Identify which keys produce a dead key (no character until the next key)
3. List exactly those legend characters in the `deadKeys` array
4. Do **not** include characters that merely look like accents but type directly (e.g. on some layouts `~` types immediately rather than acting as a dead key)

The `deadKeys` metadata gates **both** the CSS dead key indicator **and** charMap composition generation. Layouts without `deadKeys` produce no compositions — this is critical because characters like `^` and `~` are normal direct-output keys on many layouts (e.g. en-US). Getting this wrong would cause paste to send dead key sequences where the target OS expects direct output (e.g. `^a` instead of `â`).

## References

- [KLE format reference](https://github.com/ijprest/keyboard-layout-editor/wiki/Serialized-Data-Format)
- [HID Usage Table: USB HID Usage Tables 1.3, Keyboard/Keypad Page (0x07) for scancode reference](https://usb.org/sites/default/files/hut1_3_0.pdf)