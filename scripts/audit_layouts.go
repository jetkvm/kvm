//go:build ignore

// audit-layouts validates built-in KLE layout files against their
// kbdlayout.info reference. Each layout's "kbdLayoutInfo" metadata field
// is used to derive the reference download URL; the reference is then parsed
// with the same keyboard parser so the comparison is semantic (charMap and
// legend layers), not textual.
//
// Usage:
//
//	go run ./scripts/audit_layouts.go [flags] [locale ...]
//
// If no locale arguments are given, all built-in layouts are audited.
//
// Flags:
//
//	-v          Verbose: print per-scancode legend diffs and charMap deltas.
//	-layouts    Directory containing *.kle.json files.
//	            Default: internal/keyboard/layouts
//	-cache      Directory for caching downloaded reference JSON.
//	            Default: $TMPDIR/kbdlayout-cache
//	-refresh    Re-download references even if a cached copy exists.
//	-scancodes  Validate local HID scancodes against matching kbdlayout.info KLC data.
//
// Exit codes:
//
//	0  All audited layouts pass (missing entries = exit 1).
//	1  One or more layouts are missing characters or AltGr layers vs reference.
//	2  Usage / setup error.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jetkvm/kvm/internal/keyboard"
)

// ---------------------------------------------------------------------------
// Flags
// ---------------------------------------------------------------------------

var (
	flagVerbose = flag.Bool("v", false, "verbose: print per-scancode and charMap diffs")
	flagLayouts = flag.String("layouts", "internal/keyboard/layouts", "directory containing *.kle.json files")
	flagCache   = flag.String("cache", "", "cache directory for downloaded references (empty = use OS temp dir)")
	flagRefresh = flag.Bool("refresh", false, "re-download references even if cached")
	flagSC      = flag.Bool("scancodes", false, "validate local HID scancodes against kbdlayout.info KLC data")
)

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: go run ./scripts/audit_layouts.go [flags] [locale ...]\n\n")
	flag.PrintDefaults()
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Examples:")
	fmt.Fprintln(os.Stderr, "  go run ./scripts/audit_layouts.go                   # audit all layouts")
	fmt.Fprintln(os.Stderr, "  go run ./scripts/audit_layouts.go -v de_DE          # verbose audit of de_DE")
	fmt.Fprintln(os.Stderr, "  go run ./scripts/audit_layouts.go -refresh fr_BE    # force re-download")
	fmt.Fprintln(os.Stderr, "  go run ./scripts/audit_layouts.go -scancodes en_US  # validate scancodes against KLC data")
}

// ---------------------------------------------------------------------------
// Raw KLE top-object metadata
// ---------------------------------------------------------------------------

type kleTopObject struct {
	KbdLayoutInfo string `json:"kbdLayoutInfo"`
}

// extractMeta reads the first element of a KLE JSON array, which is an object
// containing board-level metadata.
func extractMeta(raw []byte) (kleTopObject, error) {
	var outer []json.RawMessage
	if err := json.Unmarshal(raw, &outer); err != nil {
		return kleTopObject{}, fmt.Errorf("unmarshal outer array: %w", err)
	}
	if len(outer) == 0 {
		return kleTopObject{}, fmt.Errorf("empty KLE array")
	}
	var meta kleTopObject
	// Not all first elements are objects; ignore unmarshal errors here.
	_ = json.Unmarshal(outer[0], &meta)
	return meta, nil
}

// ---------------------------------------------------------------------------
// Download / cache helpers
// ---------------------------------------------------------------------------

func resolvedCacheDir() string {
	if *flagCache != "" {
		return *flagCache
	}
	return filepath.Join(os.TempDir(), "kbdlayout-cache")
}

func fetchReference(infoURL string) ([]byte, error) {
	// Derive download URL: https://kbdlayout.info/XXXX/ -> .../download/json
	base := strings.TrimSuffix(infoURL, "/")
	dlURL := base + "/download/json"

	// Cache key = last path segment of base URL.
	parts := strings.Split(base, "/")
	cacheKey := parts[len(parts)-1]
	cacheFile := filepath.Join(resolvedCacheDir(), cacheKey+".json")

	if !*flagRefresh {
		if data, err := os.ReadFile(cacheFile); err == nil {
			return data, nil
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, dlURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request %s: %w", dlURL, err)
	}
	req.Header.Set("User-Agent", "audit-layouts/1.0 (jetkvm layout validation tool)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", dlURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", dlURL, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body from %s: %w", dlURL, err)
	}

	// Best-effort cache write.
	if err := os.MkdirAll(resolvedCacheDir(), 0o755); err == nil {
		_ = os.WriteFile(cacheFile, data, 0o644)
	}
	return data, nil
}

// reUnquotedKey matches unquoted JSON object keys, e.g. {x:1} or {a:6,y:0.5}.
var reUnquotedKey = regexp.MustCompile(`([{,]\s*)([a-zA-Z_][a-zA-Z0-9_]*)(\s*:)`)

var (
	// charMap entries for \\ and | commonly swap between 0x31 and 0x32
	// across ANSI and ISO variants of an otherwise equivalent layout.
	reISOANSIBackslashSwap = regexp.MustCompile(`^charMap "\\\\": ref=([0-9a-f]{2}):[0-9a-f]{2} local=([0-9a-f]{2}):[0-9a-f]{2}$`)
	reISOANSIPipeSwap      = regexp.MustCompile(`^charMap "\|": ref=([0-9a-f]{2}):[0-9a-f]{2} local=([0-9a-f]{2}):[0-9a-f]{2}$`)
)

// normalizeKLEJSON converts KLE's JavaScript-style unquoted object keys to
// valid JSON. kbdlayout.info serves files with {x:1,y:0.5} syntax.
func normalizeKLEJSON(raw []byte) []byte {
	return reUnquotedKey.ReplaceAll(raw, []byte(`${1}"${2}"${3}`))
}

type layers struct{ N, S, A, SA string }

func sv(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// printableSC returns true for scancodes that carry typed text.
func printableSC(sc uint8) bool {
	return (sc >= 0x04 && sc <= 0x38) || (sc >= 0x57 && sc <= 0x63)
}

// controlSC returns true for scancodes whose only "legend" is a display name
// (Esc, Enter, Backspace, Tab, Space). These work by scancode not charMap, so
// legend differences are cosmetic and excluded from audit comparisons.
func controlSC(sc uint8) bool {
	switch sc {
	case 0x28, // Return / Enter
		0x29, // Escape
		0x2A, // Backspace
		0x2B, // Tab
		0x2C, // Space
		0x39, // Caps Lock
		0x46, // PrintScreen / SysReq
		0x47, // Scroll Lock
		0x48, // Pause / Break
		0x53, // Num Lock
		0x58: // KP Enter
		return true
	}
	return false
}

func legendsByScancode(l *keyboard.KeyboardLayout) map[uint8]layers {
	m := make(map[uint8]layers, len(l.Keys))
	for _, k := range l.Keys {
		if k.Decal || !printableSC(k.Scancode) {
			continue
		}
		m[k.Scancode] = layers{
			N:  sv(k.Legends.Normal),
			S:  sv(k.Legends.Shift),
			A:  sv(k.Legends.AltGr),
			SA: sv(k.Legends.ShiftAltGr),
		}
	}
	return m
}

func comboSig(c keyboard.HIDCombo) string {
	if c.Prefix == nil {
		return fmt.Sprintf("%02x:%02x", c.Scancode, c.Modifiers)
	}
	return fmt.Sprintf("%02x:%02x<%s", c.Scancode, c.Modifiers, comboSig(*c.Prefix))
}

// Audit result

type diffKind int

const (
	// diffMissing: reference has entry, local does not → FAIL.
	diffMissing diffKind = iota
	// diffScancode: KLC scancode expectation mismatch → FAIL.
	diffScancode
	// diffAllowed: known, explicitly allow-listed expected divergence.
	diffAllowed
	// diffLegend: legend value differs → WARN (informational).
	diffLegend
	// diffRemap: charMap entry exists in both but via different HID combo → WARN.
	diffRemap
	// diffExtra: local has entry reference does not → INFO only.
	diffExtra
)

type finding struct {
	kind    diffKind
	subject string
}

type auditResult struct {
	locale   string
	findings []finding
	err      error
}

// pass returns true when there are no missing-entry failures.
func (r auditResult) pass() bool {
	if r.err != nil {
		return false
	}
	for _, f := range r.findings {
		if f.kind == diffMissing || f.kind == diffScancode {
			return false
		}
	}
	return true
}

// warn returns true when the layout passes but has notable informational differences.
func (r auditResult) warn() bool {
	if !r.pass() {
		return false
	}
	for _, f := range r.findings {
		if f.kind == diffLegend || f.kind == diffRemap {
			return true
		}
	}
	return false
}

func allowReason(f finding) string {
	if f.kind == diffExtra && strings.HasPrefix(f.subject, "sc 0x32: key absent in local") {
		return "expected ISO/ANSI difference (ISO-specific key 0x32)"
	}

	if f.kind == diffLegend && (strings.HasPrefix(f.subject, "sc 0x31 ") || strings.HasPrefix(f.subject, "sc 0x32 ")) {
		return "expected ISO/ANSI legend placement difference (0x31/0x32)"
	}

	if f.kind == diffRemap {
		const deadKeySpaceMarker = ` local=2c:00<`
		if idx := strings.Index(f.subject, deadKeySpaceMarker); idx >= 0 {
			prefix := f.subject[:idx]
			refIdx := strings.Index(prefix, "ref=")
			if refIdx >= 0 {
				refSig := prefix[refIdx+4:]
				localSig := strings.TrimSuffix(f.subject[idx+len(deadKeySpaceMarker):], ">")
				if refSig == localSig {
					return "expected dead-key standalone output via dead-key+Space"
				}
			}
		}

		if m := reISOANSIBackslashSwap.FindStringSubmatch(f.subject); len(m) == 3 {
			if (m[1] == "31" && m[2] == "32") || (m[1] == "32" && m[2] == "31") {
				return "expected ISO/ANSI remap for \\ (0x31/0x32)"
			}
		}
		if m := reISOANSIPipeSwap.FindStringSubmatch(f.subject); len(m) == 3 {
			if (m[1] == "31" && m[2] == "32") || (m[1] == "32" && m[2] == "31") {
				return "expected ISO/ANSI remap for | (0x31/0x32)"
			}
		}
	}

	return ""
}

func applyAllowList(findings []finding) []finding {
	out := make([]finding, 0, len(findings))
	for _, f := range findings {
		reason := allowReason(f)
		if reason == "" {
			out = append(out, f)
			continue
		}
		out = append(out, finding{
			kind:    diffAllowed,
			subject: fmt.Sprintf("%s [%s]", f.subject, reason),
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// Core audit logic
// ---------------------------------------------------------------------------

func auditLocale(locale, localPath string) auditResult {
	res := auditResult{locale: locale}

	localRaw, err := os.ReadFile(localPath)
	if err != nil {
		res.err = fmt.Errorf("read local file: %w", err)
		return res
	}

	meta, err := extractMeta(localRaw)
	if err != nil {
		res.err = fmt.Errorf("extract metadata: %w", err)
		return res
	}
	if meta.KbdLayoutInfo == "" {
		res.err = fmt.Errorf("no kbdLayoutInfo metadata in %s", localPath)
		return res
	}

	refRaw, err := fetchReference(meta.KbdLayoutInfo)
	if err != nil {
		res.err = fmt.Errorf("fetch reference: %w", err)
		return res
	}

	localLayout, err := keyboard.ParseKLE(localRaw, locale, "")
	if err != nil {
		res.err = fmt.Errorf("parse local layout: %w", err)
		return res
	}
	refLayout, err := keyboard.ParseKLE(normalizeKLEJSON(refRaw), locale, "")
	if err != nil {
		res.err = fmt.Errorf("parse reference layout: %w", err)
		return res
	}

	localLeg := legendsByScancode(localLayout)
	refLeg := legendsByScancode(refLayout)

	// Collect all scancodes present in reference.
	refSCs := make([]int, 0, len(refLeg))
	for sc := range refLeg {
		refSCs = append(refSCs, int(sc))
	}
	sort.Ints(refSCs)

	for _, sci := range refSCs {
		sc := uint8(sci)
		ref := refLeg[sc]
		local, ok := localLeg[sc]
		if !ok {
			// Entire key absent from local (e.g. ISO-only key on an ANSI layout).
			// Treat as informational rather than a hard fail.
			res.findings = append(res.findings, finding{
				kind:    diffExtra,
				subject: fmt.Sprintf("sc 0x%02X: key absent in local (ref N=%q S=%q A=%q SA=%q)", sc, ref.N, ref.S, ref.A, ref.SA),
			})
			continue
		}

		// Normal / Shift legend comparison — skip for control keys whose legends
		// are just display names (Esc, ⏎, ⌫, etc.), not typed characters.
		if !controlSC(sc) {
			if ref.N != "" && ref.N != local.N {
				res.findings = append(res.findings, finding{
					kind:    diffLegend,
					subject: fmt.Sprintf("sc 0x%02X normal: ref=%q local=%q", sc, ref.N, local.N),
				})
			}
			// Numpad shift legends are cosmetic in source data and vary by
			// convention (e.g. + in both slots vs normal-only). Treat them as
			// non-semantic to avoid noisy WARNs.
			if !isNumpadHID(sc) && ref.S != "" && ref.S != local.S {
				res.findings = append(res.findings, finding{
					kind:    diffLegend,
					subject: fmt.Sprintf("sc 0x%02X shift:  ref=%q local=%q", sc, ref.S, local.S),
				})
			}
		}

		// AltGr layer: missing is a hard fail; wrong value is a warning.
		if ref.A != "" && local.A == "" {
			res.findings = append(res.findings, finding{
				kind:    diffMissing,
				subject: fmt.Sprintf("sc 0x%02X altgr:  ref has %q but local is empty", sc, ref.A),
			})
		} else if ref.A != "" && ref.A != local.A {
			res.findings = append(res.findings, finding{
				kind:    diffLegend,
				subject: fmt.Sprintf("sc 0x%02X altgr:  ref=%q local=%q", sc, ref.A, local.A),
			})
		}

		// ShiftAltGr layer: same policy.
		if ref.SA != "" && local.SA == "" {
			res.findings = append(res.findings, finding{
				kind:    diffMissing,
				subject: fmt.Sprintf("sc 0x%02X shift+altgr: ref has %q but local is empty", sc, ref.SA),
			})
		} else if ref.SA != "" && ref.SA != local.SA {
			res.findings = append(res.findings, finding{
				kind:    diffLegend,
				subject: fmt.Sprintf("sc 0x%02X shift+altgr: ref=%q local=%q", sc, ref.SA, local.SA),
			})
		}

		// Extra local AltGr layers (informational).
		if ref.A == "" && local.A != "" {
			res.findings = append(res.findings, finding{
				kind:    diffExtra,
				subject: fmt.Sprintf("sc 0x%02X altgr:  local has %q but ref is empty", sc, local.A),
			})
		}
		if ref.SA == "" && local.SA != "" {
			res.findings = append(res.findings, finding{
				kind:    diffExtra,
				subject: fmt.Sprintf("sc 0x%02X shift+altgr: local has %q but ref is empty", sc, local.SA),
			})
		}
	}

	// charMap: check that every character the reference can type is reachable locally.
	// Missing = FAIL; different combo = WARN (character is still reachable).
	for ch, refCombo := range refLayout.CharMap {
		localCombo, ok := localLayout.CharMap[ch]
		if !ok {
			res.findings = append(res.findings, finding{
				kind:    diffMissing,
				subject: fmt.Sprintf("charMap missing %q (ref combo %s)", ch, comboSig(refCombo)),
			})
		} else if comboSig(localCombo) != comboSig(refCombo) {
			res.findings = append(res.findings, finding{
				kind:    diffRemap,
				subject: fmt.Sprintf("charMap %q: ref=%s local=%s", ch, comboSig(refCombo), comboSig(localCombo)),
			})
		}
	}

	// Extra chars in local (informational).
	for ch := range localLayout.CharMap {
		if _, ok := refLayout.CharMap[ch]; !ok {
			res.findings = append(res.findings, finding{
				kind:    diffExtra,
				subject: fmt.Sprintf("charMap extra %q (not in reference)", ch),
			})
		}
	}

	if *flagSC {
		scFindings, err := auditKLCScancodes(localRaw, localLayout)
		if err != nil {
			res.err = fmt.Errorf("KLC scancode audit: %w", err)
			return res
		}
		res.findings = append(res.findings, scFindings...)
	}

	res.findings = applyAllowList(res.findings)

	return res
}

// KLC scancode audit

// The KLC format uses PS/2 scancodes, so we need to map those to HID usage codes for comparison.
// These maps are based on the standard PS/2 set 1 scancodes and their common E0 extensions, as
// documented in various sources including the Linux kernel source and USB HID usage tables.
var ps2ToHID = map[uint8]uint8{
	0x01: 0x29, // Esc
	0x02: 0x1E, // 1
	0x03: 0x1F, // 2
	0x04: 0x20, // 3
	0x05: 0x21, // 4
	0x06: 0x22, // 5
	0x07: 0x23, // 6
	0x08: 0x24, // 7
	0x09: 0x25, // 8
	0x0A: 0x26, // 9
	0x0B: 0x27, // 0
	0x0C: 0x2D, // Minus
	0x0D: 0x2E, // Equal
	0x0E: 0x2A, // Backspace
	0x0F: 0x2B, // Tab
	0x10: 0x14, // Q
	0x11: 0x1A, // W
	0x12: 0x08, // E
	0x13: 0x15, // R
	0x14: 0x17, // T
	0x15: 0x1C, // Y
	0x16: 0x18, // U
	0x17: 0x0C, // I
	0x18: 0x12, // O
	0x19: 0x13, // P
	0x1A: 0x2F, // LeftBracket
	0x1B: 0x30, // RightBracket
	0x1C: 0x28, // Enter
	0x1D: 0xE0, // LCtrl
	0x1E: 0x04, // A
	0x1F: 0x16, // S
	0x20: 0x07, // D
	0x21: 0x09, // F
	0x22: 0x0A, // G
	0x23: 0x0B, // H
	0x24: 0x0D, // J
	0x25: 0x0E, // K
	0x26: 0x0F, // L
	0x27: 0x33, // Semicolon
	0x28: 0x34, // Quote
	0x29: 0x35, // Grave
	0x2A: 0xE1, // LShift
	0x2B: 0x31, // Backslash
	0x2C: 0x1D, // Z
	0x2D: 0x1B, // X
	0x2E: 0x06, // C
	0x2F: 0x19, // V
	0x30: 0x05, // B
	0x31: 0x11, // N
	0x32: 0x10, // M
	0x33: 0x36, // Comma
	0x34: 0x37, // Period
	0x35: 0x38, // Slash
	0x36: 0xE5, // RShift
	0x37: 0x55, // KP *
	0x38: 0xE2, // LAlt
	0x39: 0x2C, // Space
	0x3A: 0x39, // CapsLock
	0x3B: 0x3A, // F1
	0x3C: 0x3B, // F2
	0x3D: 0x3C, // F3
	0x3E: 0x3D, // F4
	0x3F: 0x3E, // F5
	0x40: 0x3F, // F6
	0x41: 0x40, // F7
	0x42: 0x41, // F8
	0x43: 0x42, // F9
	0x44: 0x43, // F10
	0x45: 0x53, // NumLock
	0x46: 0x47, // ScrollLock
	0x47: 0x5F, // KP 7
	0x48: 0x60, // KP 8
	0x49: 0x61, // KP 9
	0x4A: 0x56, // KP -
	0x4B: 0x5C, // KP 4
	0x4C: 0x5D, // KP 5
	0x4D: 0x5E, // KP 6
	0x4E: 0x57, // KP +
	0x4F: 0x59, // KP 1
	0x50: 0x5A, // KP 2
	0x51: 0x5B, // KP 3
	0x52: 0x62, // KP 0
	0x53: 0x63, // KP .
	0x56: 0x64, // NonUSBackslash
	0x57: 0x44, // F11
	0x58: 0x45, // F12
}

// E0-extended scancodes: the base scancode is 0xE0, and the actual scancode is the second byte.
var ps2E0ToHID = map[uint8]uint8{
	0x1C: 0x58, // KP Enter
	0x1D: 0xE4, // RCtrl
	0x35: 0x54, // KP /
	0x37: 0x46, // PrintScreen
	0x38: 0xE6, // RAlt (AltGr)
	0x47: 0x4A, // Home
	0x48: 0x52, // ArrowUp
	0x49: 0x4B, // PageUp
	0x4B: 0x50, // ArrowLeft
	0x4D: 0x4F, // ArrowRight
	0x4F: 0x4D, // End
	0x50: 0x51, // ArrowDown
	0x51: 0x4E, // PageDown
	0x52: 0x49, // Insert
	0x53: 0x4C, // Delete
	0x5B: 0xE3, // LGUI
	0x5C: 0xE7, // RGUI
	0x5D: 0x65, // Application
}

func parseKLCScancodeToHID(scHex string) (uint8, bool) {
	scHex = strings.TrimSpace(strings.ToUpper(scHex))
	if len(scHex) == 2 {
		scVal, err := strconv.ParseUint(scHex, 16, 8)
		if err != nil {
			return 0, false
		}
		hid, ok := ps2ToHID[uint8(scVal)]
		return hid, ok
	}
	if len(scHex) == 4 && strings.HasPrefix(scHex, "E0") {
		extVal, err := strconv.ParseUint(scHex[2:], 16, 8)
		if err != nil {
			return 0, false
		}
		hid, ok := ps2E0ToHID[uint8(extVal)]
		return hid, ok
	}
	return 0, false
}

func fetchKLC(infoURL string) ([]byte, error) {
	base := strings.TrimSuffix(infoURL, "/")
	parts := strings.Split(base, "/")
	cacheKey := parts[len(parts)-1]
	cacheFile := filepath.Join(resolvedCacheDir(), cacheKey+".klc")

	if !*flagRefresh {
		if data, err := os.ReadFile(cacheFile); err == nil {
			return data, nil
		}
	}

	dlURL := base + "/download/klc"
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, dlURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request %s: %w", dlURL, err)
	}
	req.Header.Set("User-Agent", "audit-layouts/1.0 (jetkvm layout scancode validator)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", dlURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: HTTP %d", dlURL, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body from %s: %w", dlURL, err)
	}

	if err := os.MkdirAll(resolvedCacheDir(), 0o755); err == nil {
		_ = os.WriteFile(cacheFile, data, 0o644)
	}
	return data, nil
}

func parseKLCCharToHID(data []byte) (map[rune]uint8, map[rune]bool, map[uint8]bool, error) {
	charToHID := make(map[rune]uint8)
	ambiguous := make(map[rune]bool)
	charToFirstHID := make(map[rune]uint8)
	expectedHIDs := make(map[uint8]bool)

	raw := normalizeKLCEncoding(data)
	inLayout := false
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "//"); idx >= 0 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		upper := strings.ToUpper(line)
		if upper == "LAYOUT" {
			inLayout = true
			continue
		}
		if inLayout && isKLCSectionKeyword(upper) && upper != "LAYOUT" {
			inLayout = false
			continue
		}
		if !inLayout {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		hid, ok := parseKLCScancodeToHID(fields[0])
		if !ok {
			continue
		}
		expectedHIDs[hid] = true

		for _, col := range fields[3:] {
			if col == "-1" || strings.HasSuffix(col, "@") {
				continue
			}
			cp, err := strconv.ParseUint(col, 16, 32)
			if err != nil {
				continue
			}
			r := rune(cp)
			if !isPrintableRune(r) {
				continue
			}
			if prev, seen := charToFirstHID[r]; seen {
				if prev != hid {
					ambiguous[r] = true
					delete(charToHID, r)
				}
				continue
			}
			charToFirstHID[r] = hid
			if !ambiguous[r] {
				charToHID[r] = hid
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, nil, err
	}
	return charToHID, ambiguous, expectedHIDs, nil
}

func normalizeKLCEncoding(data []byte) []byte {
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE {
		u16 := make([]uint16, (len(data)-2)/2)
		for i := range u16 {
			u16[i] = uint16(data[2+i*2]) | uint16(data[2+i*2+1])<<8
		}
		var out bytes.Buffer
		for _, u := range u16 {
			var tmp [utf8.UTFMax]byte
			n := utf8.EncodeRune(tmp[:], rune(u))
			out.Write(tmp[:n])
		}
		return out.Bytes()
	}
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		u16 := make([]uint16, (len(data)-2)/2)
		for i := range u16 {
			u16[i] = uint16(data[2+i*2])<<8 | uint16(data[2+i*2+1])
		}
		var out bytes.Buffer
		for _, u := range u16 {
			var tmp [utf8.UTFMax]byte
			n := utf8.EncodeRune(tmp[:], rune(u))
			out.Write(tmp[:n])
		}
		return out.Bytes()
	}
	return data
}

func isKLCSectionKeyword(s string) bool {
	switch s {
	case "KBD", "COPYRIGHT", "COMPANY", "LOCALENAME", "LOCALEID",
		"VERSION", "SHIFTSTATE", "ATTRIBUTES", "LAYOUT",
		"DEADKEY", "KEYNAME", "KEYNAME_EXT", "KEYNAME_DEAD",
		"DESCRIPTIONS", "LANGUAGENAMES", "ENDKBD":
		return true
	}
	return false
}

func isPrintableRune(r rune) bool {
	if r == utf8.RuneError || r == 0 {
		return false
	}
	if unicode.IsControl(r) || !unicode.IsPrint(r) {
		return false
	}
	return true
}

func isNumpadHID(sc uint8) bool {
	return sc >= 0x53 && sc <= 0x63
}

// Numpad glyphs often duplicate main-cluster characters in KLC char maps.
// For those glyphs, char->HID lookups are ambiguous and should not produce
// strict scancode mismatches when checking a numpad key.
func isAmbiguousNumpadRune(r rune) bool {
	switch r {
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '.', ',', '+', '-', '*', '/':
		return true
	}
	return false
}

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func hasEquivalentISOANSIKey(set map[uint8]bool, hid uint8) bool {
	if hid == 0x31 || hid == 0x32 || hid == 0x64 {
		return set[0x31] || set[0x32] || set[0x64]
	}
	return set[hid]
}

func auditKLCScancodes(localRaw []byte, localLayout *keyboard.KeyboardLayout) ([]finding, error) {
	meta, err := extractMeta(localRaw)
	if err != nil {
		return nil, err
	}
	if meta.KbdLayoutInfo == "" {
		return nil, fmt.Errorf("no kbdLayoutInfo metadata")
	}
	klcRaw, err := fetchKLC(meta.KbdLayoutInfo)
	if err != nil {
		return nil, err
	}
	charToHID, _, expectedHIDs, err := parseKLCCharToHID(klcRaw)
	if err != nil {
		return nil, err
	}

	findings := make([]finding, 0)
	localHIDs := make(map[uint8]bool)
	for _, key := range localLayout.Keys {
		if key.Decal {
			continue
		}
		if key.Scancode != 0 {
			localHIDs[key.Scancode] = true
		}
		deadSlots := map[string]bool{}
		for _, slot := range key.DeadLegends {
			deadSlots[slot] = true
		}
		type slot struct {
			name string
			ptr  *string
		}
		for _, sl := range []slot{
			{name: "normal", ptr: key.Legends.Normal},
			{name: "shift", ptr: key.Legends.Shift},
			{name: "altgr", ptr: key.Legends.AltGr},
			{name: "shift-altgr", ptr: key.Legends.ShiftAltGr},
		} {
			if sl.ptr == nil || deadSlots[sl.name] {
				continue
			}
			legend := *sl.ptr
			if len([]rune(legend)) != 1 {
				continue
			}
			r := firstRune(legend)
			if !isPrintableRune(r) {
				continue
			}
			expected, ok := charToHID[r]
			if !ok {
				continue
			}
			if isNumpadHID(key.Scancode) && !isNumpadHID(expected) && isAmbiguousNumpadRune(r) {
				continue
			}
			if (key.Scancode == 0x31 || key.Scancode == 0x32 || key.Scancode == 0x64) &&
				(expected == 0x31 || expected == 0x32 || expected == 0x64) {
				continue
			}
			if key.Scancode != expected {
				findings = append(findings, finding{
					kind:    diffScancode,
					subject: fmt.Sprintf("KLC scancode mismatch: %q (U+%04X) at (%.2f,%.2f): local=0x%02X expected=0x%02X", legend, r, key.X, key.Y, key.Scancode, expected),
				})
			}
		}
	}

	for expected := range expectedHIDs {
		if hasEquivalentISOANSIKey(localHIDs, expected) {
			continue
		}
		findings = append(findings, finding{
			kind:    diffScancode,
			subject: fmt.Sprintf("KLC expected key missing in local layout: HID 0x%02X", expected),
		})
	}
	return findings, nil
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	flag.Usage = usage
	flag.Parse()

	layoutDir := *flagLayouts

	// Resolve layout directory relative to working dir or repo root.
	if _, err := os.Stat(layoutDir); os.IsNotExist(err) {
		wd, _ := os.Getwd()
		for dir := wd; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			candidate := filepath.Join(dir, *flagLayouts)
			if _, err := os.Stat(candidate); err == nil {
				layoutDir = candidate
				break
			}
		}
	}
	if _, err := os.Stat(layoutDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "error: layouts directory not found: %s\n", *flagLayouts)
		os.Exit(2)
	}

	// Build locale -> file map.
	entries, err := filepath.Glob(filepath.Join(layoutDir, "*.kle.json"))
	if err != nil || len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "error: no *.kle.json files in %s\n", layoutDir)
		os.Exit(2)
	}
	fileByLocale := make(map[string]string, len(entries))
	for _, path := range entries {
		base := filepath.Base(path)
		locale := strings.TrimSuffix(base, ".kle.json")
		fileByLocale[locale] = path
	}

	// Determine which locales to audit.
	requested := flag.Args()
	var locales []string
	if len(requested) > 0 {
		for _, l := range requested {
			l = strings.TrimSuffix(l, ".kle.json")
			if _, ok := fileByLocale[l]; !ok {
				fmt.Fprintf(os.Stderr, "error: unknown locale %q (no matching .kle.json)\n", l)
				os.Exit(2)
			}
			locales = append(locales, l)
		}
	} else {
		for l := range fileByLocale {
			locales = append(locales, l)
		}
		sort.Strings(locales)
	}

	// Run audits.
	passed, warned, failed := 0, 0, 0
	for _, locale := range locales {
		result := auditLocale(locale, fileByLocale[locale])

		if result.err != nil {
			fmt.Printf("ERROR  %s: %v\n", locale, result.err)
			failed++
			continue
		}

		switch {
		case !result.pass():
			fmt.Printf("FAIL   %s\n", locale)
			failed++
		case result.warn():
			fmt.Printf("WARN   %s\n", locale)
			warned++
		default:
			// Count extras for the pass line.
			extras := 0
			for _, f := range result.findings {
				if f.kind == diffExtra {
					extras++
				}
			}
			if extras > 0 {
				fmt.Printf("PASS   %s  (+%d extra vs reference)\n", locale, extras)
			} else {
				fmt.Printf("PASS   %s\n", locale)
			}
			passed++
		}

		if *flagVerbose && len(result.findings) > 0 {
			groups := map[diffKind][]string{}
			for _, f := range result.findings {
				groups[f.kind] = append(groups[f.kind], f.subject)
			}
			printGroup := func(label string, kind diffKind) {
				items := groups[kind]
				if len(items) == 0 {
					return
				}
				sort.Strings(items)
				fmt.Printf("  [%s] %d finding(s):\n", label, len(items))
				for _, s := range items {
					fmt.Printf("    %s\n", s)
				}
			}
			printGroup("FAIL: missing in local", diffMissing)
			printGroup("FAIL: KLC scancode mismatch", diffScancode)
			printGroup("allow: expected ISO/ANSI difference", diffAllowed)
			printGroup("warn: legend differs", diffLegend)
			printGroup("warn: charMap remap", diffRemap)
			printGroup("info: extra in local", diffExtra)
			fmt.Println()
		}
	}

	fmt.Printf("\n%d passed, %d warned, %d failed\n", passed, warned, failed)
	if failed > 0 {
		os.Exit(1)
	}
}
