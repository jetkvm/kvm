//go:build ignore

// validate_layout.go — validates a KLE JSON file through the keyboard parser.
//
// Usage:
//   go run scripts/validate_layout.go <path-to-kle.json> [layout-id]
//
// Examples:
//   go run scripts/validate_layout.go my-layout.kle.json
//   go run scripts/validate_layout.go my-layout.kle.json "custom-layout"
//
// Exits 0 on success, 1 on validation errors.
// Reports key count, charMap size, dead key compositions, and any issues found.

package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jetkvm/kvm/internal/keyboard"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: go run scripts/validate_layout.go <path-to-kle.json> [layout-id]\n")
		os.Exit(1)
	}

	path := os.Args[1]
	id := "validate"
	if len(os.Args) >= 3 {
		id = os.Args[2]
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading file: %v\n", err)
		os.Exit(1)
	}

	layout, err := keyboard.ParseKLE(data, id, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Validation FAILED: %v\n", err)
		os.Exit(1)
	}

	// Count dead key compositions
	deadKeyCount := 0
	for _, combo := range layout.CharMap {
		if combo.Prefix != nil {
			deadKeyCount++
		}
	}

	// Count keys with scancodes vs unknown
	mapped := 0
	unmapped := 0
	for _, k := range layout.Keys {
		if k.Scancode != 0 {
			mapped++
		} else {
			unmapped++
		}
	}

	fmt.Printf("Layout:     %s\n", layout.Name)
	if layout.Author != "" {
		fmt.Printf("Author:     %s\n", layout.Author)
	}
	fmt.Printf("ID:         %s\n", layout.ID)
	fmt.Printf("Board size: %.0f x %.0f units\n", layout.BoardW, layout.BoardH)
	fmt.Printf("Keys:       %d total (%d mapped to HID scancodes, %d unmapped)\n", len(layout.Keys), mapped, unmapped)
	fmt.Printf("CharMap:    %d entries (%d with dead key prefix)\n", len(layout.CharMap), deadKeyCount)

	// Warn about potential issues
	warnings := 0
	if unmapped > 5 {
		fmt.Printf("WARNING:    %d keys have no HID scancode — they may be in non-standard positions\n", unmapped)
		warnings++
	}

	pct := float64(mapped) / float64(len(layout.Keys)) * 100
	if pct < 80 {
		fmt.Printf("WARNING:    Only %.0f%% of keys mapped — layout may have unusual positioning\n", pct)
		warnings++
	}

	if len(layout.CharMap) < 30 {
		fmt.Printf("WARNING:    CharMap only has %d entries — expected 60+ for a full layout\n", len(layout.CharMap))
		warnings++
	}

	// Pretty-print a sample of the charMap for spot-checking
	fmt.Printf("\nSample charMap entries:\n")
	count := 0
	for char, combo := range layout.CharMap {
		if count >= 10 {
			fmt.Printf("  ... and %d more\n", len(layout.CharMap)-10)
			break
		}
		if combo.Prefix != nil {
			fmt.Printf("  %q → scancode=0x%02X mod=0x%02X (dead prefix: scancode=0x%02X mod=0x%02X)\n",
				char, combo.Scancode, combo.Modifiers, combo.Prefix.Scancode, combo.Prefix.Modifiers)
		} else {
			fmt.Printf("  %q → scancode=0x%02X mod=0x%02X\n", char, combo.Scancode, combo.Modifiers)
		}
		count++
	}

	// Output the full JSON for inspection if requested
	if os.Getenv("VERBOSE") == "1" {
		fmt.Printf("\nFull KeyboardLayout JSON:\n")
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(layout)
	}

	if warnings > 0 {
		fmt.Printf("\nValidation passed with %d warning(s)\n", warnings)
	} else {
		fmt.Printf("\nValidation passed\n")
	}
}
