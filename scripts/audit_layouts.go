//go:build ignore

// audit-layouts validates built-in KLE layout files against their
// kbdlayout.info reference. Each layout's "kbdLayoutInfo" metadata field
// is used to derive the reference download URL; the reference is then parsed
// with the same keyboard parser so the comparison is semantic (charMap and
// legend layers), not textual.
//
// Usage:
//
//	go run ./cmd/audit-layouts [flags] [locale ...]
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
//
// Exit codes:
//
//	0  All audited layouts pass (missing entries = exit 1).
//	1  One or more layouts are missing characters or AltGr layers vs reference.
//	2  Usage / setup error.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

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
)

func usage() {
	fmt.Fprintf(os.Stderr, "Usage: go run ./cmd/audit-layouts [flags] [locale ...]\n\n")
	flag.PrintDefaults()
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Examples:")
	fmt.Fprintln(os.Stderr, "  go run ./cmd/audit-layouts                   # audit all layouts")
	fmt.Fprintln(os.Stderr, "  go run ./cmd/audit-layouts -v de_DE          # verbose audit of de_DE")
	fmt.Fprintln(os.Stderr, "  go run ./cmd/audit-layouts -refresh fr_BE    # force re-download")
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

// ---------------------------------------------------------------------------
// KLE JSON normalization
// ---------------------------------------------------------------------------

// reUnquotedKey matches unquoted JSON object keys, e.g. {x:1} or {a:6,y:0.5}.
var reUnquotedKey = regexp.MustCompile(`([{,]\s*)([a-zA-Z_][a-zA-Z0-9_]*)(\s*:)`)

// normalizeKLEJSON converts KLE's JavaScript-style unquoted object keys to
// valid JSON. kbdlayout.info serves files with {x:1,y:0.5} syntax.
func normalizeKLEJSON(raw []byte) []byte {
	return reUnquotedKey.ReplaceAll(raw, []byte(`${1}"${2}"${3}`))
}

// ---------------------------------------------------------------------------
// Semantic comparison helpers
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Audit result
// ---------------------------------------------------------------------------

type diffKind int

const (
	// diffMissing: reference has entry, local does not → FAIL.
	diffMissing diffKind = iota
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
		if f.kind == diffMissing {
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
			if ref.S != "" && ref.S != local.S {
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

	return res
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
