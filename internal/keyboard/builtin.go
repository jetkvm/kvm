package keyboard

import (
	"embed"
	"fmt"
	"path"
	"strings"
)

//go:embed layouts/*.kle.json
var layoutFS embed.FS

// layoutAliases maps alternative IDs to the actual KLE filename stem.
// Used when the canonical config ID doesn't match the file name.
var layoutAliases = map[string]string{
	"nl-BE": "fr_BE", // Belgian AZERTY — isoCode was "nl-BE" in the old system
}

// builtinLayouts is computed once at startup by scanning the embedded
// layouts/ directory. Each .kle.json file becomes a hyphenated ID
// (e.g. en_US.kle.json → "en-US"). Aliases are added on top.
var builtinLayouts = discoverBuiltinLayouts()

func discoverBuiltinLayouts() map[string]struct{} {
	layouts := make(map[string]struct{})

	entries, err := layoutFS.ReadDir("layouts")
	if err != nil {
		return layouts
	}

	const suffix = ".kle.json"
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, suffix) {
			continue
		}
		// en_US.kle.json → "en-US"
		stem := strings.TrimSuffix(name, suffix)
		id := strings.ReplaceAll(stem, "_", "-")
		layouts[id] = struct{}{}
	}

	// Add aliases
	for alias := range layoutAliases {
		layouts[alias] = struct{}{}
	}

	return layouts
}

// loadBuiltinLayoutFromFS reads a built-in KLE JSON from the embedded filesystem
// and parses it into a KeyboardLayout.
//
// The canonical layout IDs use hyphens (e.g. "en-US") to match existing device
// configs, while the KLE files on disk use underscores (en_US.kle.json).
// This function handles the conversion, plus any aliases.
func loadBuiltinLayoutFromFS(id string) (*KeyboardLayout, error) {
	// Check for aliases first (e.g. nl-BE → fr_BE)
	fileStem := ""
	if alias, ok := layoutAliases[id]; ok {
		fileStem = alias
	} else {
		// Convert hyphens to underscores: "en-US" → "en_US"
		fileStem = strings.ReplaceAll(id, "-", "_")
	}

	filename := path.Join("layouts", fileStem+".kle.json")
	data, err := layoutFS.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("built-in layout not found: %s", id)
	}
	return ParseKLE(data, id, "")
}
