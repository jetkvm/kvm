package keyboard

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
)

// SpecialKey is one entry in the canonical key-alias taxonomy.
//
// The taxonomy is the single source of truth for which legend strings on a
// non-text key (or Space) refer to which logical key. Both the Go keycap
// display normalization (controlLegendDisplayMap) and the TypeScript aria-label
// resolution (KEY_ARIA_NAMES in Keycap.tsx) are derived from this data.
type SpecialKey struct {
	// AriaKey is the suffix used by the frontend to look up the localized aria
	// name (m.keys_<AriaKey>()).
	AriaKey string `json:"ariaKey"`
	// Canonical is the preferred display form for keycaps.
	Canonical string `json:"canonical"`
	// Aliases are alternate legend strings that should be normalized to
	// Canonical for display purposes. The Canonical itself is implicitly
	// recognized and need not appear here.
	Aliases []string `json:"aliases"`
}

//go:embed keyaliases.json
var keyAliasesJSON []byte

// SpecialKeys is the parsed taxonomy. Read-only after init.
var SpecialKeys []SpecialKey

// PassthroughLegendPattern matches multi-character legends that are
// self-explanatory across keyboards (e.g. F1–F24) and need no aria translation.
var PassthroughLegendPattern *regexp.Regexp

// controlLegendDisplayMap is the alias→canonical lookup used by
// normalizeControlLegendsForDisplay. Built from SpecialKeys at init.
var controlLegendDisplayMap map[string]string

// IsKnownSpecialLegend reports whether legend is recognized by the taxonomy:
// either as a canonical form, a declared alias, or a passthrough match (F-keys).
func IsKnownSpecialLegend(legend string) bool {
	if _, ok := controlLegendDisplayMap[legend]; ok {
		return true
	}
	if PassthroughLegendPattern != nil && PassthroughLegendPattern.MatchString(legend) {
		return true
	}
	return false
}

func init() {
	var doc struct {
		SpecialKeys              []SpecialKey `json:"specialKeys"`
		PassthroughLegendPattern string       `json:"passthroughLegendPattern"`
	}
	if err := json.Unmarshal(keyAliasesJSON, &doc); err != nil {
		panic(fmt.Sprintf("keyaliases.json: parse failed: %v", err))
	}

	SpecialKeys = doc.SpecialKeys
	controlLegendDisplayMap = make(map[string]string, len(SpecialKeys)*4)

	// Verify uniqueness as we build: every alias and canonical must map to
	// exactly one SpecialKey. Duplicates would silently corrupt the lookup.
	for _, sk := range SpecialKeys {
		if sk.AriaKey == "" || sk.Canonical == "" {
			panic(fmt.Sprintf("keyaliases.json: entry with empty ariaKey or canonical: %+v", sk))
		}
		if existing, dup := controlLegendDisplayMap[sk.Canonical]; dup && existing != sk.Canonical {
			panic(fmt.Sprintf("keyaliases.json: canonical %q collides with alias of %q", sk.Canonical, existing))
		}
		controlLegendDisplayMap[sk.Canonical] = sk.Canonical
		for _, alias := range sk.Aliases {
			if existing, dup := controlLegendDisplayMap[alias]; dup {
				panic(fmt.Sprintf("keyaliases.json: alias %q used by both %q and %q", alias, existing, sk.Canonical))
			}
			controlLegendDisplayMap[alias] = sk.Canonical
		}
	}

	if doc.PassthroughLegendPattern != "" {
		re, err := regexp.Compile(doc.PassthroughLegendPattern)
		if err != nil {
			panic(fmt.Sprintf("keyaliases.json: invalid passthroughLegendPattern: %v", err))
		}
		PassthroughLegendPattern = re
	}
}
