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

// SpecialKeys is the parsed taxonomy. Read-only after package load.
//
// PassthroughLegendPattern matches multi-character legends that are
// self-explanatory across keyboards (e.g. F1–F24) and need no aria translation.
//
// controlLegendDisplayMap is the alias→canonical lookup used by
// normalizeControlLegendsForDisplay; built from SpecialKeys.
//
// All three are initialised together at package load time via parseKeyAliases.
// A parse failure during var-initializer panics: the JSON is embedded in the
// binary, so a failure here means a broken build, not a recoverable runtime error.
var (
	SpecialKeys              []SpecialKey
	PassthroughLegendPattern *regexp.Regexp
	controlLegendDisplayMap  map[string]string

	_ = func() struct{} {
		SpecialKeys, PassthroughLegendPattern, controlLegendDisplayMap = parseKeyAliases(keyAliasesJSON)
		return struct{}{}
	}()
)

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

func parseKeyAliases(raw []byte) ([]SpecialKey, *regexp.Regexp, map[string]string) {
	var doc struct {
		SpecialKeys              []SpecialKey `json:"specialKeys"`
		PassthroughLegendPattern string       `json:"passthroughLegendPattern"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		panic(fmt.Sprintf("keyaliases.json: parse failed: %v", err))
	}

	display := make(map[string]string, len(doc.SpecialKeys)*4)

	// Verify uniqueness as we build: every alias and canonical must map to
	// exactly one SpecialKey. Duplicates would silently corrupt the lookup.
	for _, sk := range doc.SpecialKeys {
		if sk.AriaKey == "" || sk.Canonical == "" {
			panic(fmt.Sprintf("keyaliases.json: entry with empty ariaKey or canonical: %+v", sk))
		}
		if existing, dup := display[sk.Canonical]; dup && existing != sk.Canonical {
			panic(fmt.Sprintf("keyaliases.json: canonical %q collides with alias of %q", sk.Canonical, existing))
		}
		display[sk.Canonical] = sk.Canonical
		for _, alias := range sk.Aliases {
			if existing, dup := display[alias]; dup {
				panic(fmt.Sprintf("keyaliases.json: alias %q used by both %q and %q", alias, existing, sk.Canonical))
			}
			display[alias] = sk.Canonical
		}
	}

	var pattern *regexp.Regexp
	if doc.PassthroughLegendPattern != "" {
		re, err := regexp.Compile(doc.PassthroughLegendPattern)
		if err != nil {
			panic(fmt.Sprintf("keyaliases.json: invalid passthroughLegendPattern: %v", err))
		}
		pattern = re
	}

	return doc.SpecialKeys, pattern, display
}
